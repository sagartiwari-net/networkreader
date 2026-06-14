package main

import "testing"

func TestAzadseoLoginSucceeded(t *testing.T) {
	if !azadseoLoginSucceeded("https://members.azadseo.com/member", "<html><title>Dashboard</title></html>") {
		t.Fatal("expected success on member dashboard")
	}
	memberLogin := `<title>Member Login - Azad Seo Tools</title><input name="amember_login">Please Login!!`
	if azadseoLoginSucceeded("https://members.azadseo.com/member", memberLogin) {
		t.Fatal("expected fail on /member login form")
	}
	failBody := `<form><input name="amember_login"><div class="am-errors">bad</div>Sign In`
	if azadseoLoginSucceeded("https://members.azadseo.com/login", failBody) {
		t.Fatal("expected fail on login page with errors")
	}
}

func TestParseAzadseoAccessibleTools(t *testing.T) {
	html := `<div class="col product-card">
		<a href="/member/go/123" class="btn btn-access">Access Tool</a>
		<span class="product-title">Semrush</span>
		<img alt="Semrush Guru" src="https://cdn.azadseo.com/semrush.png">
	</div>
	<div class="col product-card">
		<a href="/member/go/456">Launch</a>
		<span class="product-title">Ahrefs</span>
	</div>`
	info := parseAzadseoMemberPage(html)
	if len(info.Active) != 2 {
		t.Fatalf("got %+v", info)
	}
	name, label := formatAzadseoPlanResult(info)
	if name != "Semrush | Ahrefs" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseAzadseoMemberPlansNoActive(t *testing.T) {
	html := `<h3 id="no_subscription">You have no active subscriptions</h3>`
	info := parseAzadseoMemberPage(html)
	name, label := formatAzadseoPlanResult(info)
	if name != "No Active Subscription" || label != "FREE" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseAzadseoMemberPlansSubscriptionItem(t *testing.T) {
	html := `<div class="subscription-item">
		<strong>Guru Package</strong>
		<span class="text-success">Active</span>
	</div>`
	info := parseAzadseoMemberPage(html)
	name, label := formatAzadseoPlanResult(info)
	if name != "Guru Package" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestFinalizeAzadseoPlanInfoDefaultsFree(t *testing.T) {
	info := finalizeAzadseoPlanInfo(noxtoolsPlanInfo{})
	name, label := formatAzadseoPlanResult(info)
	if name != "No Active Subscription" || label != "FREE" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestParseAzadseoMemberPlansFreeMarker(t *testing.T) {
	html := `<div class="member-dashboard">Please purchase a subscription to access tools.</div>`
	info := parseAzadseoMemberPage(html)
	if !info.NoActive {
		t.Fatalf("got %+v", info)
	}
}
