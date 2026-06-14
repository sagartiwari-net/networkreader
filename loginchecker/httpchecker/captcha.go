package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type captchaSubmitResp struct {
	Status  int    `json:"status"`
	Request string `json:"request"`
}

type captchaResultResp struct {
	Status  int    `json:"status"`
	Request string `json:"request"`
}

func (c *Checker) solveSemrushRecaptcha(pageURL string) (string, error) {
	apiKey := strings.TrimSpace(c.cfg.Var("twocaptcha_api_key", ""))
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("TWOCAPTCHA_API_KEY"))
	}
	if apiKey == "" {
		return "", nil
	}
	siteKey := c.cfg.Var("recaptcha_site_key", "6Ldw6DYUAAAAACFCNmvsT32P6VPVonpjbSS7XTA9")
	return solve2Captcha(apiKey, siteKey, pageURL)
}

func solve2Captcha(apiKey, siteKey, pageURL string) (string, error) {
	submitURL := fmt.Sprintf(
		"https://2captcha.com/in.php?key=%s&method=userrecaptcha&googlekey=%s&pageurl=%s&enterprise=1&json=1",
		url.QueryEscape(apiKey),
		url.QueryEscape(siteKey),
		url.QueryEscape(pageURL),
	)
	resp, err := http.Get(submitURL)
	if err != nil {
		return "", fmt.Errorf("2captcha submit: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	var sub captchaSubmitResp
	if err := json.Unmarshal(data, &sub); err != nil {
		return "", fmt.Errorf("2captcha submit parse: %w", err)
	}
	if sub.Status != 1 || sub.Request == "" {
		return "", fmt.Errorf("2captcha submit failed: %s", strings.TrimSpace(string(data)))
	}

	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		pollURL := fmt.Sprintf(
			"https://2captcha.com/res.php?key=%s&action=get&id=%s&json=1",
			url.QueryEscape(apiKey),
			url.QueryEscape(sub.Request),
		)
		pollResp, err := http.Get(pollURL)
		if err != nil {
			continue
		}
		pollData, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		var res captchaResultResp
		if err := json.Unmarshal(pollData, &res); err != nil {
			continue
		}
		if res.Status == 1 && res.Request != "" {
			return res.Request, nil
		}
		if res.Status == 0 && !strings.Contains(strings.ToUpper(res.Request), "CAPCHA_NOT_READY") {
			return "", fmt.Errorf("2captcha solve failed: %s", res.Request)
		}
	}
	return "", fmt.Errorf("2captcha timeout after 120s")
}
