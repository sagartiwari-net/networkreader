package main

import (
	"fmt"
	"net/http"
	"strings"
)

const StatusPlanInactive CheckStatus = "PLAN_INACTIVE"

func isPaidishLabel(label string) bool {
	switch label {
	case "FREE", "FREE_TRIAL":
		return false
	default:
		return true
	}
}

func detectInactiveFromAccountJSON(accountBody, planLabel string) (bool, string) {
	if !isPaidishLabel(planLabel) {
		return false, ""
	}

	paying, hasPaying := jsonPathString(accountBody, "0.account.paying")
	if hasPaying && paying == "true" {
		return false, ""
	}
	if hasPaying && paying == "false" {
		planName, _ := jsonPathString(accountBody, "0.account.plan.name")
		if planName == "" {
			planName = planLabel
		}
		return true, fmt.Sprintf("Inactive paid plan (paying=false) — %s", planName)
	}

	customer, hasCustomer := jsonPathString(accountBody, "0.account.stripe_customer_id")
	subID, hasSub := jsonPathString(accountBody, "0.account.stripe_pro_subscription_id")
	lastInvoice, hasInvoice := jsonPathString(accountBody, "0.account.last_invoice_paid_at")

	noCustomer := !hasCustomer || customer == "" || customer == "null"
	noSub := !hasSub || subID == "" || subID == "null"
	noInvoice := !hasInvoice || lastInvoice == "" || lastInvoice == "null"

	if noCustomer && noSub && noInvoice {
		planName, _ := jsonPathString(accountBody, "0.account.plan.name")
		return true, fmt.Sprintf("No billing on file — %s", planName)
	}

	return false, ""
}

func bodyIndicatesLegacyPricing(body string) bool {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "free trial expired") {
		return true
	}
	if strings.Contains(lower, "choose a plan") && strings.Contains(lower, "pricing") {
		return true
	}
	if strings.Contains(lower, "legacy") &&
		(strings.Contains(lower, "pricing plans") || strings.Contains(lower, "/pricing")) {
		return true
	}
	if url, ok := jsonPathString(body, "url"); ok {
		u := strings.ToLower(url)
		if strings.Contains(u, "/pricing") {
			return true
		}
	}
	if match := dataPageRE.FindStringSubmatch(body); len(match) >= 2 {
		decoded := strings.ToLower(htmlUnescape(match[1]))
		if strings.Contains(decoded, "/pricing") &&
			(strings.Contains(decoded, "legacy") || strings.Contains(decoded, "choose a plan")) {
			return true
		}
	}
	return false
}

func bodyIndicatesNoBilling(body string) bool {
	lower := strings.ToLower(body)
	if strings.Contains(lower, "no payment information") {
		return true
	}
	if strings.Contains(lower, "you don't have any invoices") {
		return true
	}
	if strings.Contains(lower, "you don&#039;t have any invoices") {
		return true
	}
	return false
}

func headerIndicatesPricing(headers http.Header) bool {
	if headers == nil {
		return false
	}
	for _, key := range []string{"X-Inertia-Location", "Location"} {
		val := strings.ToLower(headers.Get(key))
		if strings.Contains(val, "/pricing") {
			return true
		}
	}
	return false
}

func (c *Checker) sessionHasInactivePaidPlan(client *http.Client, planLabel string) (bool, string) {
	if !isPaidishLabel(planLabel) {
		return false, ""
	}

	urls := []string{c.cfg.PricingURL(), c.cfg.BillingURL()}
	headerSets := []map[string]string{
		{
			"Accept":     "text/html, application/xhtml+xml",
			"User-Agent": c.cfg.UserAgent,
		},
		{
			"Accept":           "text/html, application/xhtml+xml",
			"X-Inertia":        "true",
			"X-Requested-With": "XMLHttpRequest",
			"User-Agent":       c.cfg.UserAgent,
		},
	}

	for _, rawURL := range urls {
		for _, headers := range headerSets {
			body, status, respHeaders, err := c.doRequest(client, "GET", rawURL, headers, "")
			if err != nil || status != http.StatusOK {
				continue
			}
			if headerIndicatesPricing(respHeaders) || bodyIndicatesLegacyPricing(body) {
				return true, "Legacy/expired plan — pricing upgrade required"
			}
			if strings.Contains(rawURL, "billing") && bodyIndicatesNoBilling(body) {
				return true, "Paid plan label but no payment/invoices on billing page"
			}
		}
	}
	return false, ""
}

func (c *Checker) accountIsUsablePaid(client *http.Client, accountBody, planLabel string) (bool, string) {
	if inactive, reason := detectInactiveFromAccountJSON(accountBody, planLabel); inactive {
		return false, reason
	}
	planName, _ := jsonPathString(accountBody, "0.account.plan.name")
	if strings.Contains(strings.ToLower(planName), "legacy") {
		if inactive, reason := c.sessionHasInactivePaidPlan(client, planLabel); inactive {
			return false, reason
		}
	}
	return true, ""
}
