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
	paths, err := pickWindowsTextFiles(title, false)
	if err == nil {
		return paths[0], nil
	}
	paths, menuErr := pickAccountsFromMenu(title, false, err)
	if menuErr != nil {
		return "", menuErr
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no file selected")
	}
	return paths[0], nil
}

func pickAccountsFilesWindows(title string) ([]string, error) {
	paths, err := pickWindowsTextFiles(title, true)
	if err == nil {
		return paths, nil
	}
	return pickAccountsFromMenu(title, true, err)
}

func pickWindowsTextFiles(title string, multiselect bool) ([]string, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("not windows")
	}
	resultFile, err := os.CreateTemp("", "httpchecker-pick-result-*.txt")
	if err != nil {
		return nil, err
	}
	resultPath := resultFile.Name()
	resultFile.Close()
	defer os.Remove(resultPath)

	multiPS := "$false"
	if multiselect {
		multiPS = "$true"
	}
	script := fmt.Sprintf(`
$ResultFile = %q
$Title = %q
try {
  Add-Type -AssemblyName System.Windows.Forms | Out-Null
  [System.Windows.Forms.Application]::EnableVisualStyles()
  $d = New-Object System.Windows.Forms.OpenFileDialog
  $d.Title = $Title
  $d.Filter = "Text files (*.txt)|*.txt|All files (*.*)|*.*"
  $d.Multiselect = %s
  $desktop = [Environment]::GetFolderPath('Desktop')
  if (Test-Path -LiteralPath $desktop) { $d.InitialDirectory = $desktop }
  $logs = Join-Path $desktop 'logs'
  if (Test-Path -LiteralPath $logs) { $d.InitialDirectory = $logs }
  if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
    $paths = @()
    if ($null -ne $d.FileNames -and $d.FileNames.Length -gt 0) { $paths = @($d.FileNames) }
    elseif ($d.FileName) { $paths = @($d.FileName) }
    if ($paths.Count -gt 0) {
      [System.IO.File]::WriteAllLines($ResultFile, $paths)
    }
  }
} catch {
  [System.IO.File]::WriteAllText($ResultFile, ('ERROR:' + $_.Exception.Message))
}
`, resultPath, title, multiPS)

	runEmbeddedPowerShell(script)

	data, err := os.ReadFile(resultPath)
	if err != nil {
		return nil, fmt.Errorf("read picker result: %w", err)
	}
	text := strings.TrimSpace(string(data))
	if strings.HasPrefix(text, "ERROR:") {
		return nil, fmt.Errorf("%s", strings.TrimPrefix(text, "ERROR:"))
	}
	paths := splitPickerLines(text)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no file selected")
	}
	return paths, nil
}

func splitPickerLines(text string) []string {
	var paths []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.Trim(line, `"`)
		line = strings.TrimSuffix(line, "\r")
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths
}

func discoverAccountTxtFiles() []string {
	home, _ := os.UserHomeDir()
	roots := []string{
		filepath.Join(home, "Desktop", "logs"),
		filepath.Join(home, "Desktop", "logs", "filtered_results"),
		filepath.Join(home, "Desktop"),
		`C:\Users\Administrator\Desktop\logs`,
		`C:\Users\Administrator\Desktop\logs\filtered_results`,
		`C:\Users\Administrator\Desktop`,
	}
	seen := make(map[string]struct{})
	var files []string
	for _, root := range roots {
		matches, _ := filepath.Glob(filepath.Join(root, "*.txt"))
		for _, m := range matches {
			if _, ok := seen[strings.ToLower(m)]; ok {
				continue
			}
			seen[strings.ToLower(m)] = struct{}{}
			files = append(files, m)
		}
	}
	return files
}

func pickAccountsFromMenu(title string, multiselect bool, pickerErr error) ([]string, error) {
	for {
		if pickerErr != nil {
			fmt.Printf("  File picker unavailable: %v\n", pickerErr)
		}
		files := discoverAccountTxtFiles()
		if len(files) > 0 {
			fmt.Println()
			fmt.Println("  Found account files — enter number(s), comma-separated:")
			for i, f := range files {
				fmt.Printf("    %2d = %s\n", i+1, f)
			}
			fmt.Println("    B  = Open file browser again")
			fmt.Println("    A  = Select ALL listed files")
			fmt.Println("    M  = Type full path(s) manually")
			choice := readLine("  Select: ")
			switch strings.ToUpper(strings.TrimSpace(choice)) {
			case "B":
				paths, err := pickWindowsTextFiles(title, multiselect)
				if err == nil {
					return paths, nil
				}
				pickerErr = err
				continue
			case "A":
				if multiselect {
					return append([]string(nil), files...), nil
				}
				return []string{files[0]}, nil
			case "M":
				paths := readAccountsPathsManual()
				if len(paths) > 0 {
					return paths, nil
				}
				continue
			}
			if choice != "" {
				var selected []string
				for _, part := range strings.Split(choice, ",") {
					part = strings.TrimSpace(part)
					if part == "" {
						continue
					}
					idx, err := strconv.Atoi(part)
					if err != nil || idx < 1 || idx > len(files) {
						fmt.Printf("  Invalid choice: %q\n", part)
						selected = nil
						break
					}
					selected = append(selected, files[idx-1])
				}
				if len(selected) > 0 {
					if !multiselect {
						return selected[:1], nil
					}
					return selected, nil
				}
			}
		}

		fmt.Println("  Opening file browser again...")
		paths, err := pickWindowsTextFiles(title, multiselect)
		if err == nil {
			return paths, nil
		}
		pickerErr = err
		fmt.Println("  Type full path(s), comma-separated:")
		paths = readAccountsPathsManual()
		if len(paths) > 0 {
			return paths, nil
		}
		return nil, fmt.Errorf("no accounts file selected")
	}
}

func readAccountsPathsManual() []string {
	raw := readLine("  Accounts file(s): ")
	var paths []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.Trim(strings.TrimSpace(part), `"`)
		if part != "" {
			paths = append(paths, part)
		}
	}
	return paths
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
	out, err := runPowerShellFilePicker(script)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func runEmbeddedPowerShell(script string) {
	tmp, err := os.CreateTemp("", "httpchecker-picker-*.ps1")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(script); err != nil {
		tmp.Close()
		return
	}
	tmp.Close()
	_, _ = osExec("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-STA", "-File", tmpPath)
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

func runInteractive() (configPath string, accountsPaths []string, resultsDir string, workers int, proxyPath string, err error) {
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
		return "", nil, "", 0, "", fmt.Errorf("invalid config choice — enter a number between 1 and %d", len(configFiles)+1)
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
			return "", nil, "", 0, "", fmt.Errorf("no config selected")
		}
		configPath = picked
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		return "", nil, "", 0, "", err
	}

	fmt.Println()
	fmt.Printf("  Selected: %s\n", configDisplayName(configPath, cfg))
	fmt.Println()
	fmt.Println("  Select accounts file(s) (email:password, one per line)")
	fmt.Println("  Tip: Ctrl+click to select multiple files in the picker")
	fmt.Println("  Opening file picker...")
	fmt.Println()

	accountsPaths, err = pickAccountsFilesWindows("Select accounts file(s)")
	if err != nil {
		return "", nil, "", 0, "", err
	}

	for i, p := range accountsPaths {
		accountsPaths[i] = strings.Trim(p, `"`)
		if _, statErr := os.Stat(accountsPaths[i]); statErr != nil {
			return "", nil, "", 0, "", fmt.Errorf("accounts file not found: %s", accountsPaths[i])
		}
	}
	if len(accountsPaths) == 1 {
		fmt.Printf("  Selected: %s\n", accountsPaths[0])
	} else {
		fmt.Printf("  Selected %d files:\n", len(accountsPaths))
		for _, p := range accountsPaths {
			fmt.Printf("    - %s\n", p)
		}
	}

	fmt.Println()
	fmt.Println("  Proxy mode:")
	fmt.Println("    1 = Without proxy (normal — use 3-5 workers)")
	fmt.Println("    2 = With proxy (select proxy file — use workers = proxy count)")
	w := cfg.Settings.Workers
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
			return "", nil, "", 0, "", fmt.Errorf("no proxy file selected")
		}
		if _, statErr := os.Stat(proxyPath); statErr != nil {
			return "", nil, "", 0, "", fmt.Errorf("proxy file not found: %s", proxyPath)
		}
		if pool, loadErr := LoadProxyPool(proxyPath); loadErr != nil {
			return "", nil, "", 0, "", loadErr
		} else {
			fmt.Printf("  Loaded %d proxy entries\n", pool.Len())
			w = pool.SuggestedWorkers()
		}
	}

	if w <= 0 {
		w = 5
	}
	if proxyPath != "" {
		fmt.Println("  Tip: N sticky proxies → use workers = proxy count (e.g. 50 proxies → 50 workers)")
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
	return configPath, accountsPaths, resultsDir, w, proxyPath, nil
}
