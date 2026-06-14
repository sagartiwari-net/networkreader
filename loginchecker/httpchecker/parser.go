package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type Account struct {
	Email    string
	Password string
}

func ParseAccountsFile(path string) ([]Account, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open accounts file: %w", err)
	}
	defer file.Close()

	var accounts []Account
	scanner := bufio.NewScanner(file)
	lineNum := 0
	seen := make(map[string]struct{})

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}

		email := strings.TrimSpace(line[:colonIdx])
		password := strings.TrimSpace(line[colonIdx+1:])
		if email == "" || password == "" {
			continue
		}

		key := email + ":" + password
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		accounts = append(accounts, Account{Email: email, Password: password})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no valid email:password lines found")
	}
	return accounts, nil
}
