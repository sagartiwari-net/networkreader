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
