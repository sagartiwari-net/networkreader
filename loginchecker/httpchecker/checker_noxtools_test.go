package main

import "testing"

func TestParseNoxtoolsLoginAttemptID(t *testing.T) {
	html := `<input type="hidden" name="login_attempt_id" value="1781419264" />`
	id, err := parseNoxtoolsLoginAttemptID(html)
	if err != nil {
		t.Fatal(err)
	}
	if id != "1781419264" {
		t.Fatalf("got %q", id)
	}
}

func TestParseNoxtoolsLoginError(t *testing.T) {
	html := `<div class="am-errors"><div class="am-error">The user name or password is incorrect</div></div>`
	msg := parseNoxtoolsLoginError(html)
	if msg != "The user name or password is incorrect" {
		t.Fatalf("got %q", msg)
	}
}

func TestNoxtoolsLoginSucceeded(t *testing.T) {
	if noxtoolsLoginSucceeded("https://noxtools.com/secure/secure/yourwallet", "<html></html>") != true {
		t.Fatal("expected success on yourwallet")
	}
	failBody := `<form><input name="amember_login"><div class="am-errors">bad</div>Sign In`
	if noxtoolsLoginSucceeded("https://noxtools.com/secure/login", failBody) {
		t.Fatal("expected fail on login page with errors")
	}
}

func TestParseNoxtoolsMemberPlansNoActive(t *testing.T) {
	html := `<h3 id="no_subscription">You have no active subscriptions</h3>`
	info := parseNoxtoolsMemberPlans(html)
	if !info.NoActive || len(info.Active) != 0 {
		t.Fatalf("got %+v", info)
	}
	name, label := formatNoxtoolsPlanResult(info)
	if name != "No Active Subscription" || label != "FREE" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseNoxtoolsMemberPlansActive(t *testing.T) {
	html := `<div class="subscription-item">
		<strong>Semrush Guru</strong>
		<span class="text-success">Active</span>
	</div>
	<div class="subscription-item">
		<strong>SkillShare</strong>
		<span class="text-success">Active</span>
	</div>`
	info := parseNoxtoolsMemberPlans(html)
	if len(info.Active) != 2 {
		t.Fatalf("got %+v", info)
	}
	name, label := formatNoxtoolsPlanResult(info)
	if name != "Semrush Guru | SkillShare" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseNoxtoolsMemberPlansExpiredOnly(t *testing.T) {
	html := `<div class="subscription-item">
		<strong>Kwfinder</strong>
		<span class="text-danger">Expired</span>
	</div>`
	info := parseNoxtoolsMemberPlans(html)
	name, label := formatNoxtoolsPlanResult(info)
	if name != "Kwfinder (expired)" || label != "EXPIRED" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseNoxtoolsMemberPlansExpiresInDays(t *testing.T) {
	html := `<div id="member-main-subscriptions">
	<div class="subscription-item">
		<strong><span class="dot"></span>Storyblocks</strong>
		<span class="text-danger statuspill">Expires in 9 days</span>
	</div></div>`
	info := parseNoxtoolsMemberPlans(html)
	name, label := formatNoxtoolsPlanResult(info)
	if name != "Storyblocks (9 days left)" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseNoxtoolsLastPaidProduct(t *testing.T) {
	html := `<table class="am-payment-table"><tr><td>25 Feb 2026</td><td><a>15JTJ/1</a></td><td>SkillShare</td><td>Stripe</td><td>99.00 INR</td></tr></table>`
	if got := parseNoxtoolsLastPaidProduct(html); got != "SkillShare" {
		t.Fatalf("got %q", got)
	}
}
