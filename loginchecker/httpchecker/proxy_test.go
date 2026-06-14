package main

import (
	"net/url"
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

func TestParseWaitSeconds(t *testing.T) {
	if sec := parseWaitSeconds("Please wait 79 seconds before next login attempt"); sec != 79 {
		t.Fatalf("got %d", sec)
	}
	if d := cooldownForRateReason("Please wait 60 seconds before next login attempt"); d < 60*time.Second {
		t.Fatalf("cooldown too short: %v", d)
	}
}

func TestProxyPoolMaxInflight(t *testing.T) {
	pool := &ProxyPool{proxies: make([]*url.URL, 50)}
	if pool.MaxInflight() != 50 {
		t.Fatalf("got %d", pool.MaxInflight())
	}
	pool2 := &ProxyPool{proxies: make([]*url.URL, 1)}
	if pool2.MaxInflight() != 12 {
		t.Fatalf("got %d", pool2.MaxInflight())
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
