package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	configPath := flag.String("config", "", "Checker config JSON")
	accountsPath := flag.String("accounts", "", "email:password wordlist")
	resultsDir := flag.String("out", "", "Output directory (default: results/<site>/run_<timestamp>/)")
	workers := flag.Int("workers", 0, "Parallel workers (0 = use config default)")
	proxyPath := flag.String("proxy", "", "Proxy list file (host:port:user:pass per line)")
	flag.Parse()

	interactive := *configPath == "" && *accountsPath == "" && flag.NFlag() == 0
	exitCode := 0

	if interactive {
		cfgPath, accPaths, outDir, w, proxyFile, err := runInteractive()
		if err != nil {
			fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
			exitCode = 1
		} else {
			_, _, err = runChecker(RunOptions{
				ConfigPath:    cfgPath,
				AccountsPaths: accPaths,
				ResultsDir:    outDir,
				Workers:       w,
				ProxyPath:     proxyFile,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
				exitCode = 1
			}
		}
		if runtime.GOOS == "windows" {
			pauseBeforeExit()
		}
		os.Exit(exitCode)
	}

	baseDir := exeDir()
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(baseDir, "configs", "buzzsumo.json")
	} else if !filepath.IsAbs(cfgPath) {
		cfgPath = resolvePath(baseDir, cfgPath)
	}

	accPath := *accountsPath
	if accPath == "" {
		accPath = filepath.Join(baseDir, "accounts.txt")
	} else if !filepath.IsAbs(accPath) {
		accPath = resolvePath(baseDir, accPath)
	}

	outDir := *resultsDir
	if outDir != "" && !filepath.IsAbs(outDir) {
		outDir = resolvePath(baseDir, outDir)
	}

	proxyFile := *proxyPath
	if proxyFile != "" && !filepath.IsAbs(proxyFile) {
		proxyFile = resolvePath(baseDir, proxyFile)
	}

	if _, _, err := runChecker(RunOptions{
		ConfigPath:   cfgPath,
		AccountsPath: accPath,
		ResultsDir:   outDir,
		Workers:      *workers,
		ProxyPath:    proxyFile,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
