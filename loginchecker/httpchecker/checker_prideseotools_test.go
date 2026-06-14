package main

import "testing"

func TestPrideSeoToolsLoginAttemptID(t *testing.T) {
	html := `<input type="hidden" name="login_attempt_id" value="1781430344" />`
	id, err := parseNoxtoolsLoginAttemptID(html)
	if err != nil {
		t.Fatal(err)
	}
	if id != "1781430344" {
		t.Fatalf("got %q", id)
	}
}

func TestPrideSeoToolsLoginSucceeded(t *testing.T) {
	if !amemberLoginSucceeded("https://members.prideseotools.com/member", "<html><title>Dashboard</title></html>") {
		t.Fatal("expected success on member dashboard")
	}
	if amemberLoginSucceeded("https://members.prideseotools.com/login", `<input name="amember_login">Please login`) {
		t.Fatal("expected fail on login form")
	}
}

func TestPrideSeoToolsMemberPlansNoActive(t *testing.T) {
	html := `<h3 id="no_subscription">You have no active subscriptions</h3>`
	info := parseAmemberShopMemberPage(html)
	name, label := formatAmemberShopPlanResult(finalizeAmemberShopPlanInfo(info))
	if name != "No Active Subscription" || label != "FREE" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestPrideSeoToolsResourceLinks(t *testing.T) {
	html := `<li id="resource-link-link-42-wrapper">
		<a href="/protect/content/semrush-guru">Semrush Guru</a>
	</li>
	<li id="resource-link-link-99-wrapper">
		<a href="/protect/content/ahrefs">Ahrefs</a>
	</li>`
	info := parseAmemberShopMemberPage(html)
	name, label := formatAmemberShopPlanResult(info)
	if name != "Semrush Guru | Ahrefs" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestPrideSeoToolsMemberPlansActive(t *testing.T) {
	html := `<div class="subscription-item">
		<strong>Lite Plan</strong>
		<span class="text-success">Active</span>
	</div>`
	info := parseAmemberShopMemberPage(html)
	name, label := formatAmemberShopPlanResult(info)
	if name != "Lite Plan" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}
