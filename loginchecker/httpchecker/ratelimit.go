package main

import (
	"net/http"
	"strings"
	"time"
)

func isRateLimitErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "status code 429") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "malformed http status") ||
		strings.Contains(msg, "rate limit")
}

func isRateLimitedStatus(status int) bool {
	return status == http.StatusTooManyRequests
}

func rateLimitReason(context string) string {
	return context + " — rate limited (BuzzSumo or proxy). Retry rate_limited.txt with 10-15 workers"
}

func (c *Checker) resultFromRequestErr(context string, err error) (CheckStatus, string) {
	if isRateLimitErr(err) {
		return StatusRateLimited, rateLimitReason(context)
	}
	return StatusError, err.Error()
}

func proxyRetrySleep(attempt int) {
	secs := 1 << attempt
	if secs > 8 {
		secs = 8
	}
	time.Sleep(time.Duration(secs) * time.Second)
}
