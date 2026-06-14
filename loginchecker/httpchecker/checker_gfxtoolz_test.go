package main

import "testing"

func TestGfxtoolzLoginSucceeded(t *testing.T) {
	body := `{"accessToken":"eyJhbG","refreshToken":"rt","user":{"email":"a@b.com"}}`
	if !gfxtoolzLoginSucceeded(200, body) {
		t.Fatal("expected success")
	}
	if gfxtoolzLoginSucceeded(401, body) {
		t.Fatal("expected fail on 401")
	}
}

func TestParseGfxtoolzLoginFail(t *testing.T) {
	body := `{"statusCode":401,"code":"INVALID_CREDENTIALS","message":"Invalid email or password"}`
	if got := parseGfxtoolzLoginFail(body); got != "Invalid email or password" {
		t.Fatalf("got %q", got)
	}
}

func TestGfxtoolzDeviceFingerprint(t *testing.T) {
	fp := gfxtoolzDeviceFingerprint("Test@Example.com")
	if len(fp) < 8 || len(fp) > 256 {
		t.Fatalf("bad fp len %d", len(fp))
	}
	if gfxtoolzDeviceFingerprint("Test@Example.com") != fp {
		t.Fatal("expected stable fingerprint")
	}
}
