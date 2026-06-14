package main

import "testing"

func TestParseHindseoLoginAttemptID(t *testing.T) {
	html := `<input type="hidden" name="login_attempt_id" value="1781429507" />`
	id, err := parseNoxtoolsLoginAttemptID(html)
	if err != nil {
		t.Fatal(err)
	}
	if id != "1781429507" {
		t.Fatalf("got %q", id)
	}
}

func TestHindseoLoginSucceeded(t *testing.T) {
	if !amemberLoginSucceeded("https://access.hindseo.com/member", "<html><title>Tools</title></html>") {
		t.Fatal("expected success on member dashboard")
	}
	if amemberLoginSucceeded("https://access.hindseo.com/login", `<input name="amember_login">Please login`) {
		t.Fatal("expected fail on login form")
	}
}

func TestParseHindseoMemberPlansNoActive(t *testing.T) {
	html := `<h3 id="no_subscription">You have no active subscriptions</h3>`
	info := parseAmemberShopMemberPage(html)
	name, label := formatAmemberShopPlanResult(finalizeAmemberShopPlanInfo(info))
	if name != "No Active Subscription" || label != "FREE" {
		t.Fatalf("got %q / %q", name, label)
	}
}
