package main

import "testing"

func TestAmemberLoginSucceeded(t *testing.T) {
	if !amemberLoginSucceeded("https://access.hindseo.com/member", "<html><title>Dashboard</title></html>") {
		t.Fatal("expected success on member dashboard")
	}
	memberLogin := `<title>Please login</title><input name="amember_login">Please Login!!`
	if amemberLoginSucceeded("https://access.hindseo.com/member", memberLogin) {
		t.Fatal("expected fail on /member login form")
	}
	failBody := `<form><input name="amember_login"><div class="am-errors">bad</div>Sign In`
	if amemberLoginSucceeded("https://access.hindseo.com/login", failBody) {
		t.Fatal("expected fail on login page with errors")
	}
}

func TestParseAmemberShopAccessibleTools(t *testing.T) {
	html := `<div class="col product-card">
		<a href="/member/go/123" class="btn btn-access">Access Tool</a>
		<span class="product-title">Semrush</span>
	</div>
	<div class="col product-card">
		<a href="/member/go/456">Launch</a>
		<span class="product-title">Ahrefs</span>
	</div>`
	info := parseAmemberShopMemberPage(html)
	if len(info.Active) != 2 {
		t.Fatalf("got %+v", info)
	}
	name, label := formatAmemberShopPlanResult(info)
	if name != "Semrush | Ahrefs" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseAmemberShopMemberPlansNoActive(t *testing.T) {
	html := `<h3 id="no_subscription">You have no active subscriptions</h3>`
	info := parseAmemberShopMemberPage(html)
	name, label := formatAmemberShopPlanResult(info)
	if name != "No Active Subscription" || label != "FREE" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseAmemberShopMemberPlansSubscriptionItem(t *testing.T) {
	html := `<div class="subscription-item">
		<strong>Guru Package</strong>
		<span class="text-success">Active</span>
	</div>`
	info := parseAmemberShopMemberPage(html)
	name, label := formatAmemberShopPlanResult(info)
	if name != "Guru Package" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestFinalizeAmemberShopPlanInfoDefaultsFree(t *testing.T) {
	info := finalizeAmemberShopPlanInfo(noxtoolsPlanInfo{})
	name, label := formatAmemberShopPlanResult(info)
	if name != "No Active Subscription" || label != "FREE" {
		t.Fatalf("got %q / %q", name, label)
	}
}
