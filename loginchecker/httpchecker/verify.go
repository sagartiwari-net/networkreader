package main

import (
	"net/http"
	"strings"
)

func bodyIndicatesFacebookVerify(body string) bool {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "please verify your account") {
		return true
	}
	if strings.Contains(lower, "connect your facebook account") {
		return true
	}
	if strings.Contains(lower, "connect my facebook account") {
		return true
	}
	if strings.Contains(lower, "/verify") &&
		(strings.Contains(lower, "facebook") || strings.Contains(lower, "connect")) {
		return true
	}
	if url, ok := jsonPathString(body, "url"); ok {
		if strings.Contains(strings.ToLower(url), "/verify") {
			return true
		}
	}
	if component, ok := jsonPathString(body, "component"); ok {
		comp := strings.ToLower(component)
		if strings.Contains(comp, "verify") {
			return true
		}
	}
	if match := dataPageRE.FindStringSubmatch(body); len(match) >= 2 {
		decoded := strings.ToLower(htmlUnescape(match[1]))
		if strings.Contains(decoded, "/verify") &&
			(strings.Contains(decoded, "facebook") || strings.Contains(decoded, "please verify")) {
			return true
		}
	}
	return false
}

func htmlUnescape(s string) string {
	return strings.NewReplacer("&quot;", `"`, "&#039;", "'", "&amp;", "&").Replace(s)
}

func headerIndicatesFacebookVerify(headers http.Header) bool {
	if headers == nil {
		return false
	}
	for _, key := range []string{"X-Inertia-Location", "Location"} {
		val := strings.ToLower(headers.Get(key))
		if strings.Contains(val, "/verify") {
			return true
		}
	}
	return false
}

func (c *Checker) loginNeedsFacebookVerify(client *http.Client, postBody string, postHeaders http.Header) bool {
	if headerIndicatesFacebookVerify(postHeaders) || bodyIndicatesFacebookVerify(postBody) {
		return true
	}
	return c.sessionRequiresFacebookVerify(client)
}

func (c *Checker) sessionRequiresFacebookVerify(client *http.Client) bool {
	urls := []string{c.cfg.VerifyURL(), c.cfg.BaseURL() + "/"}
	headerSets := []map[string]string{
		{
			"Accept":     "text/html, application/xhtml+xml",
			"User-Agent": c.cfg.UserAgent,
		},
		{
			"Accept":             "text/html, application/xhtml+xml",
			"X-Inertia":          "true",
			"X-Requested-With":   "XMLHttpRequest",
			"User-Agent":         c.cfg.UserAgent,
		},
	}
	for _, rawURL := range urls {
		for _, headers := range headerSets {
			body, status, respHeaders, err := c.doRequest(client, "GET", rawURL, headers, "")
			if err != nil || status != http.StatusOK {
				continue
			}
			if headerIndicatesFacebookVerify(respHeaders) || bodyIndicatesFacebookVerify(body) {
				return true
			}
		}
	}
	return false
}

func (c *Checker) enrichPlanFromAccount(client *http.Client, result *CheckResult) {
	body, status, _, err := c.doRequest(client, "GET", c.cfg.AccountQueryURL(), map[string]string{
		"Accept":           "application/json, text/plain, */*",
		"Cache-Control":    "no-cache",
		"Referer":          c.cfg.BaseURL() + "/",
		"X-Requested-With": "XMLHttpRequest",
		"User-Agent":       c.cfg.UserAgent,
	}, "")
	if err != nil || status != http.StatusOK {
		return
	}
	result.AccountEmail, _ = jsonPathString(body, "0.email")
	result.FirstName, _ = jsonPathString(body, "0.first_name")
	result.LastName, _ = jsonPathString(body, "0.last_name")
	result.PlanName, _ = jsonPathString(body, "0.account.plan.name")
	result.PlanID, _ = jsonPathString(body, "0.account.plan_id")
	result.StripePlanID, _ = jsonPathString(body, "0.account.plan.stripe_plan_id")
	if result.PlanName != "" {
		result.PlanLabel = classifyPlan(c.cfg, *result)
	}
}
