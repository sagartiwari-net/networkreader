package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const minSemrushProxyLifetime = 30 * time.Second

var ipRoyalLifetimeRE = regexp.MustCompile(`(?i)_lifetime-(\d+)([smhd])`)

// ipRoyalSessionLifetime parses IPRoyal password suffix _lifetime-3s / _lifetime-10m.
func ipRoyalSessionLifetime(password string) (warning string, lifetime time.Duration, ok bool) {
	m := ipRoyalLifetimeRE.FindStringSubmatch(password)
	if len(m) != 3 {
		return "", 0, false
	}
	n, err := parseInt(m[1])
	if err != nil || n <= 0 {
		return "", 0, false
	}
	switch strings.ToLower(m[2]) {
	case "s":
		lifetime = time.Duration(n) * time.Second
	case "m":
		lifetime = time.Duration(n) * time.Minute
	case "h":
		lifetime = time.Duration(n) * time.Hour
	case "d":
		lifetime = time.Duration(n) * 24 * time.Hour
	default:
		return "", 0, false
	}
	if lifetime < minSemrushProxyLifetime {
		warning = fmt.Sprintf(
			"IPRoyal lifetime %s is too short for Semrush login (~6-15s per account). Use _lifetime-10m or higher.",
			m[0],
		)
	}
	return warning, lifetime, true
}

func parseInt(s string) (int, error) {
	var n int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

func proxySessionLabel(u *url.URL) string {
	if u == nil || u.User == nil {
		return "direct"
	}
	pass, _ := u.User.Password()
	if idx := strings.Index(pass, "_session-"); idx >= 0 {
		rest := pass[idx+len("_session-"):]
		if end := strings.Index(rest, "_"); end > 0 {
			return rest[:end]
		}
		return rest
	}
	return u.User.Username()
}

func checkProxyEgress(proxyURL *url.URL, timeout time.Duration) (ip string, err error) {
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy:              http.ProxyURL(proxyURL),
			DisableKeepAlives:  true,
			DisableCompression: true,
		},
	}
	req, err := http.NewRequest(http.MethodGet, "https://api.ipify.org?format=text", nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

// printProxyStartupReport verifies a sample of proxies and warns about short IPRoyal lifetimes.
func printProxyStartupReport(pool *ProxyPool, siteID string) {
	if pool == nil || pool.Len() == 0 {
		return
	}
	fmt.Println()
	fmt.Println("  Proxy check:")

	seenWarn := map[string]struct{}{}
	for _, u := range pool.proxies {
		if u == nil || u.User == nil {
			continue
		}
		pass, _ := u.User.Password()
		if warn, _, ok := ipRoyalSessionLifetime(pass); ok && warn != "" {
			if _, dup := seenWarn[warn]; !dup {
				fmt.Printf("  ⚠ %s\n", warn)
				seenWarn[warn] = struct{}{}
			}
		}
	}

	sampleN := pool.Len()
	if sampleN > 3 {
		sampleN = 3
	}
	timeout := 20 * time.Second
	for i := 0; i < sampleN; i++ {
		u := pool.proxies[i]
		label := proxySessionLabel(u)
		ip, err := checkProxyEgress(u, timeout)
		if err != nil {
			fmt.Printf("  ✗ session %s → %v\n", label, err)
			continue
		}
		fmt.Printf("  ✓ session %s → egress IP %s\n", label, ip)
	}
	if siteID == "semrush" {
		fmt.Println("  Tip: Semrush login needs same IP for all steps — use IPRoyal _lifetime-10m (not 3s).")
	}
	fmt.Println()
}
