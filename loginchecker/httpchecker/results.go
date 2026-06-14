package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func siteResultsRoot(baseDir string, cfg *Config) string {
	siteID := strings.TrimSpace(cfg.ID)
	if siteID == "" {
		siteID = "unknown"
	}
	return filepath.Join(baseDir, "results", siteID)
}

func newRunResultsDir(baseDir string, cfg *Config) string {
	siteRoot := siteResultsRoot(baseDir, cfg)
	stamp := time.Now().Format("2006-01-02_150405")
	return filepath.Join(siteRoot, "run_"+stamp)
}

func writeRunSummary(path string, cfg *Config, opts RunOptions, stats RunStats, elapsed time.Duration) error {
	content := fmt.Sprintf(`HTTP Account Checker Summary
=============================
Site         : %s (%s)
Config       : %s
Accounts file: %s
Total lines  : %d
Workers      : %d
Delay (ms)   : %d
Elapsed      : %s

Results
-------
HIT           : %d  (valid login — see hits.txt)
FAIL          : %d  (wrong password — see invalid.txt)
FB VERIFY     : %d  (login ok but Facebook verify — see facebook_verify.txt)
INACTIVE      : %d  (paid label but no billing/legacy — see inactive_plan.txt)
CAPTCHA       : %d  (reCAPTCHA wall — see recaptcha_required.txt)
RATE LIMITED  : %d  (HTTP 429 — retry rate_limited.txt with 3 workers)
ERROR         : %d  (network/other — see errors.txt)

Folders
-------
by_plan/      plan-wise hits (PRO, FREE_TRIAL, PAID...)
hits.txt      all valid logins with active plan name
paid.txt      non-free plans
free_trial.txt free / trial plans
facebook_verify.txt login ok but needs Facebook connect (ignored)
inactive_plan.txt paid name but no subscription/billing (ignored)
invalid.txt   bad credentials
rate_limited.txt accounts blocked by BuzzSumo — run again later
errors.txt    other failures
`,
		cfg.Name,
		cfg.ID,
		opts.ConfigPath,
		opts.AccountsPath,
		stats.Total,
		opts.Workers,
		cfg.Settings.DelayMS,
		elapsed.Round(time.Millisecond),
		stats.Hits,
		stats.Fails,
		stats.VerifySkip,
		stats.InactiveSkip,
		stats.CaptchaSkip,
		stats.RateLimited,
		stats.Errors,
	)
	return os.WriteFile(path, []byte(content), 0o644)
}

type RunStats struct {
	Total        int
	Hits         int64
	Fails        int64
	VerifySkip   int64
	InactiveSkip int64
	CaptchaSkip  int64
	RateLimited  int64
	Errors       int64
}
