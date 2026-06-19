package main

import (
	"strings"
	"testing"
)

func TestParseSpinrewriterLoginFields(t *testing.T) {
	html := `<input type="hidden" id="login_token" name="login_token" value="abc123token" />
<input type="hidden" id="checksum" name="checksum" value="4459" />`
	token, checksum, err := parseSpinrewriterLoginFields(html)
	if err != nil {
		t.Fatal(err)
	}
	if token != "abc123token" || checksum != "4459" {
		t.Fatalf("got token=%q checksum=%q", token, checksum)
	}
}

func TestParseSpinrewriterLoginError(t *testing.T) {
	html := `<div class="alert  alert--error"><div class="alert__content">Invalid email or password.</div></div>`
	if msg := parseSpinrewriterLoginError(html); msg != "Invalid email or password." {
		t.Fatalf("got %q", msg)
	}
}

func TestSpinrewriterLoginSucceeded(t *testing.T) {
	if !spinrewriterLoginSucceeded("https://www.spinrewriter.com/cp-account", "<html><title>My Account</title></html>") {
		t.Fatal("expected success on cp-account")
	}
	if spinrewriterLoginSucceeded("https://www.spinrewriter.com/log-in", `<div id="container-log-in"><input name="login_token"></div>`) {
		t.Fatal("expected fail on login page")
	}
	if spinrewriterLoginSucceeded("https://www.spinrewriter.com/log-in", `<div class="alert  alert--error">Invalid email or password.</div>`) {
		t.Fatal("expected fail on invalid credentials")
	}
}

func TestParseSpinrewriterAccountPageFree(t *testing.T) {
	html := `<div class="pricing__item"><button class="paddle_button_js">Get Monthly Access</button></div>
<div class="pricing__item"><button class="paddle_button_js">Get Yearly Access</button></div>`
	info := parseSpinrewriterAccountPage(html)
	if info.Label != "FREE" {
		t.Fatalf("got label %q", info.Label)
	}
}

func TestParseSpinrewriterAccountPagePaidBraintree(t *testing.T) {
	html := `<a class="click_cancel_braintree_subscription" href="/action/cancel-subscription">Cancel subscription</a>
<p>Your plan: Spin Rewriter - Monthly</p>
<p>$47.00 per month</p>`
	info := parseSpinrewriterAccountPage(html)
	if info.Label != "PAID" || !strings.Contains(info.Name, "Monthly") {
		t.Fatalf("got %q / %q", info.Name, info.Label)
	}
}

func TestParseSpinrewriterAccountPagePaidPaddle(t *testing.T) {
	html := `<a class="click_cancel_paddle_subscription" data-override="https://checkout.paddle.com/cancel">Cancel</a>`
	info := parseSpinrewriterAccountPage(html)
	if info.Label != "PAID" {
		t.Fatalf("got label %q", info.Label)
	}
}

func TestParseSpinrewriterAccountPageFreeTrial(t *testing.T) {
	html := `<p>Your free trial ends in 3 days left</p>
<a class="click_cancel_paddle_subscription">Cancel your free trial</a>`
	info := parseSpinrewriterAccountPage(html)
	if info.Label != "FREE_TRIAL" {
		t.Fatalf("got label %q", info.Label)
	}
}
