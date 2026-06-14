package main

import (
	"testing"
	"time"
)

func TestParseProxyLineIPRoyal(t *testing.T) {
	line := "geo.iproyal.com:12321:user123:pass123_session-utC2YDdu_lifetime-3s"
	u, err := ParseProxyLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "geo.iproyal.com:12321" {
		t.Fatalf("host = %q", u.Host)
	}
	user := u.User.Username()
	pass, _ := u.User.Password()
	if user != "user123" {
		t.Fatalf("user = %q", user)
	}
	wantPass := "pass123_session-utC2YDdu_lifetime-3s"
	if pass != wantPass {
		t.Fatalf("pass = %q want %q", pass, wantPass)
	}
}

func TestParseProxyLinePasswordWithColons(t *testing.T) {
	line := "host:8080:admin:secret:extra:bit"
	u, err := ParseProxyLine(line)
	if err != nil {
		t.Fatal(err)
	}
	pass, _ := u.User.Password()
	if pass != "secret:extra:bit" {
		t.Fatalf("pass = %q", pass)
	}
}

func TestIPRoyalLifetimeWarning(t *testing.T) {
	warn, d, ok := ipRoyalSessionLifetime("pass_session-abc_lifetime-3s")
	if !ok || d != 3*time.Second {
		t.Fatalf("got ok=%v d=%v", ok, d)
	}
	if warn == "" {
		t.Fatal("expected warning for 3s lifetime")
	}
	_, d, ok = ipRoyalSessionLifetime("pass_session-abc_lifetime-10m")
	if !ok || d != 10*time.Minute {
		t.Fatalf("got ok=%v d=%v", ok, d)
	}
}
