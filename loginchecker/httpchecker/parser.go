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

func ParseAccountsFiles(paths []string) ([]Account, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no accounts files provided")
	}
	seen := make(map[string]struct{})
	var accounts []Account
	for _, path := range paths {
		batch, err := ParseAccountsFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		for _, acc := range batch {
			key := acc.Email + ":" + acc.Password
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			accounts = append(accounts, acc)
		}
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no valid email:password lines found")
	}
	return accounts, nil
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

func loadRunAccounts(opts RunOptions) ([]Account, error) {
	paths := opts.AccountsPaths
	if len(paths) == 0 && opts.AccountsPath != "" {
		paths = []string{opts.AccountsPath}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("accounts: no accounts file provided")
	}
	if len(paths) == 1 {
		accounts, err := ParseAccountsFile(paths[0])
		if err != nil {
			return nil, fmt.Errorf("accounts: %w", err)
		}
		return accounts, nil
	}
	accounts, err := ParseAccountsFiles(paths)
	if err != nil {
		return nil, fmt.Errorf("accounts: %w", err)
	}
	return accounts, nil
}

func formatAccountsSource(opts RunOptions) string {
	paths := opts.AccountsPaths
	if len(paths) == 0 && opts.AccountsPath != "" {
		return opts.AccountsPath
	}
	if len(paths) == 1 {
		return paths[0]
	}
	return fmt.Sprintf("%d files merged", len(paths))
}
