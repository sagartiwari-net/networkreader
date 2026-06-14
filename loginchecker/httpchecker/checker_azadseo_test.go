package main

import "testing"

func TestAzadseoLoginSucceeded(t *testing.T) {
	if !azadseoLoginSucceeded("https://members.azadseo.com/member", "<html></html>") {
		t.Fatal("expected success on /member")
	}
	failBody := `<form><input name="amember_login"><div class="am-errors">bad</div>Sign In`
	if azadseoLoginSucceeded("https://members.azadseo.com/login", failBody) {
		t.Fatal("expected fail on login page with errors")
	}
	if azadseoLoginSucceeded("https://members.azadseo.com/login", `<input name="amember_login">Sign In`) {
		t.Fatal("expected fail when still on login form")
	}
}

func TestMergeAzadseoGroupBuyTools(t *testing.T) {
	html := `<a href="/member/go/123">Access</a>
	<div class="tool-title">Semrush</div>
	<div class="tool-title">Ahrefs</div>`
	info := mergeAzadseoGroupBuyTools(noxtoolsPlanInfo{}, html)
	if len(info.Active) != 2 {
		t.Fatalf("got %+v", info)
	}
	name, label := formatAzadseoPlanResult(info)
	if name != "Semrush | Ahrefs" || label != "PAID" {
		t.Fatalf("got %q / %q", name, label)
	}
}

func TestMergeAzadseoGroupBuyToolsSkipsNoActive(t *testing.T) {
	html := `<a href="/member/go/123">Access</a><div class="tool-title">Semrush</div>`
	info := mergeAzadseoGroupBuyTools(noxtoolsPlanInfo{NoActive: true}, html)
	if len(info.Active) != 0 || !info.NoActive {
		t.Fatalf("got %+v", info)
	}
}

func TestParseAzadseoMemberPlansNoActive(t *testing.T) {
	html := `<h3 id="no_subscription">You have no active subscriptions</h3>`
	info := parseNoxtoolsMemberPlans(html)
	name, label := formatAzadseoPlanResult(info)
	if name != "No Active Subscription" || label != "FREE" {
		t.Fatalf("got %q / %q", name, label)
	}
}
