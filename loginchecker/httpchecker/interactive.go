package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(exe))
	if err != nil {
		return filepath.Dir(exe)
	}
	return dir
}

func resolvePath(baseDir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}

func listConfigFiles(baseDir string) ([]string, error) {
	configDir := filepath.Join(baseDir, "configs")
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		files = append(files, filepath.Join(configDir, e.Name()))
	}
	return files, nil
}

func readLine(prompt string) string {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func pauseBeforeExit() {
	fmt.Print("\nPress Enter to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}

func pickAccountsFileWindows(title string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("not windows")
	}
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Application]::EnableVisualStyles()
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = %q
$d.Filter = "Text files (*.txt)|*.txt|All files (*.*)|*.*"
$d.InitialDirectory = [Environment]::GetFolderPath('Desktop')
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
	Write-Output $d.FileName
}
`, title)
	return runPowerShellFilePicker(script)
}

func pickConfigFileWindows(title, initialDir string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", fmt.Errorf("not windows")
	}
	script := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
[System.Windows.Forms.Application]::EnableVisualStyles()
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Title = %q
$d.Filter = "JSON config (*.json)|*.json|All files (*.*)|*.*"
$d.InitialDirectory = %q
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
	Write-Output $d.FileName
}
`, title, initialDir)
	return runPowerShellFilePicker(script)
}

func runPowerShellFilePicker(script string) (string, error) {
	// Write temp script next to exe so InitialDirectory works when needed.
	tmp, err := os.CreateTemp("", "httpchecker-picker-*.ps1")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()

	out, err := osExec("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-STA", "-File", tmpPath)
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("no file selected")
	}
	return path, nil
}

// osExec is overridden in tests; default uses os/exec.
var osExec = defaultExec

func defaultExec(name string, args ...string) ([]byte, error) {
	// Implemented in exec_windows.go / exec_unix.go
	return execCommand(name, args...)
}

func configDisplayName(path string, cfg *Config) string {
	name := filepath.Base(path)
	if cfg != nil && cfg.Name != "" {
		return fmt.Sprintf("%s (%s)", cfg.Name, name)
	}
	return name
}

func runInteractive() (configPath, accountsPath, resultsDir string, workers int, proxyPath string, err error) {
	baseDir := exeDir()
	resultsDir = "" // auto: results/<site>/run_<timestamp>/
	proxyPath = ""

	fmt.Println()
	fmt.Println("  ========================================================")
	fmt.Println("  |        HTTP Account Checker — Interactive Mode       |")
	fmt.Println("  ========================================================")
	fmt.Println()

	configFiles, listErr := listConfigFiles(baseDir)
	if listErr != nil {
		fmt.Printf("[WARN] configs folder not found beside exe: %v\n", listErr)
	}

	fmt.Println("  Select site config:")
	if len(configFiles) == 0 {
		fmt.Println("    (no configs/*.json found — use custom path)")
	} else {
		for i, p := range configFiles {
			label := filepath.Base(p)
			if cfg, loadErr := LoadConfig(p); loadErr == nil {
				label = configDisplayName(p, cfg)
			}
			fmt.Printf("    %d = %s\n", i+1, label)
		}
	}
	fmt.Printf("    %d = Browse for custom config file...\n", len(configFiles)+1)
	fmt.Println()

	choice := readLine(fmt.Sprintf("  Enter 1-%d [1]: ", len(configFiles)+1))
	if choice == "" {
		choice = "1"
	}
	idx, convErr := strconv.Atoi(choice)
	if convErr != nil || idx < 1 || idx > len(configFiles)+1 {
		return "", "", "", 0, "", fmt.Errorf("invalid config choice — enter a number between 1 and %d", len(configFiles)+1)
	}

	if idx <= len(configFiles) {
		configPath = configFiles[idx-1]
	} else {
		fmt.Println()
		fmt.Println("  Opening config file picker...")
		picked, pickErr := pickConfigFileWindows("Select checker config JSON", filepath.Join(baseDir, "configs"))
		if pickErr != nil {
			fmt.Println("  File picker unavailable — type full path:")
			picked = readLine("  Config path: ")
		}
		if picked == "" {
			return "", "", "", 0, "", fmt.Errorf("no config selected")
		}
		configPath = picked
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		return "", "", "", 0, "", err
	}

	fmt.Println()
	fmt.Printf("  Selected: %s\n", configDisplayName(configPath, cfg))
	fmt.Println()
	fmt.Println("  Select accounts file (email:password, one per line)")
	fmt.Println("  Opening file picker...")
	fmt.Println()

	accountsPath, err = pickAccountsFileWindows("Select accounts file (email:password)")
	if err != nil {
		fmt.Println("  File picker cancelled or unavailable.")
		fmt.Println("  Type full path to accounts .txt file:")
		accountsPath = readLine("  Accounts file: ")
	}
	if accountsPath == "" {
		return "", "", "", 0, "", fmt.Errorf("no accounts file selected")
	}

	accountsPath = strings.Trim(accountsPath, `"`)
	if _, statErr := os.Stat(accountsPath); statErr != nil {
		return "", "", "", 0, "", fmt.Errorf("accounts file not found: %s", accountsPath)
	}

	fmt.Println()
	fmt.Println("  Proxy mode:")
	fmt.Println("    1 = Without proxy (normal — use 3-5 workers)")
	fmt.Println("    2 = With proxy (select proxy file — use 10-15 workers for 1 rotating proxy)")
	proxyChoice := readLine("  Enter 1-2 [1]: ")
	if proxyChoice == "" {
		proxyChoice = "1"
	}
	if proxyChoice == "2" {
		fmt.Println()
		fmt.Println("  Select proxy file (one proxy per line: host:port:user:pass)")
		proxyPath, err = pickAccountsFileWindows("Select proxy list file")
		if err != nil {
			fmt.Println("  File picker unavailable — type full path:")
			proxyPath = readLine("  Proxy file: ")
		}
		proxyPath = strings.Trim(proxyPath, `"`)
		if proxyPath == "" {
			return "", "", "", 0, "", fmt.Errorf("no proxy file selected")
		}
		if _, statErr := os.Stat(proxyPath); statErr != nil {
			return "", "", "", 0, "", fmt.Errorf("proxy file not found: %s", proxyPath)
		}
		if pool, loadErr := LoadProxyPool(proxyPath); loadErr != nil {
			return "", "", "", 0, "", loadErr
		} else {
			fmt.Printf("  Loaded %d proxy entries\n", pool.Len())
		}
	}

	w := cfg.Settings.Workers
	if w <= 0 {
		w = 5
	}
	if proxyPath != "" && w <= 5 {
		w = 12
	}
	if proxyPath != "" {
		fmt.Println("  Tip: 1 rotating proxy — use 10-15 workers (not 40+). More = proxy/BuzzSumo 429.")
	} else {
		fmt.Println("  Tip: Without proxy — BuzzSumo par 3-5 workers use karo (HTTP 429)")
	}
	wInput := readLine(fmt.Sprintf("  Workers [%d]: ", w))
	if wInput != "" {
		if parsed, parseErr := strconv.Atoi(wInput); parseErr == nil && parsed > 0 {
			w = parsed
		}
	}

	fmt.Println()
	return configPath, accountsPath, resultsDir, w, proxyPath, nil
}
