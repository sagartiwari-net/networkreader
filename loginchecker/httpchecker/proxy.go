package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

var waitSecondsRE = regexp.MustCompile(`(?i)wait\s+(\d+)\s+seconds?`)

type ProxyPool struct {
	proxies       []*url.URL
	idx           atomic.Uint64
	cooldownUntil []atomic.Int64 // unix nanos; 0 = ready
}

func LoadProxyPool(path string) (*ProxyPool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open proxy file: %w", err)
	}
	defer f.Close()

	var proxies []*url.URL
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parsed, err := ParseProxyLine(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		proxies = append(proxies, parsed)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("no proxies found in %s", path)
	}
	pool := &ProxyPool{proxies: proxies, cooldownUntil: make([]atomic.Int64, len(proxies))}
	return pool, nil
}

func ParseProxyLine(line string) (*url.URL, error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, fmt.Errorf("empty proxy line")
	}
	if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
		u, err := url.Parse(line)
		if err != nil {
			return nil, err
		}
		if u.Host == "" {
			return nil, fmt.Errorf("invalid proxy URL")
		}
		return u, nil
	}
	if strings.Contains(line, "@") {
		u, err := url.Parse("http://" + line)
		if err != nil {
			return nil, err
		}
		return u, nil
	}

	parts := strings.SplitN(line, ":", 4)
	switch len(parts) {
	case 2:
		u, err := url.Parse(fmt.Sprintf("http://%s:%s", parts[0], parts[1]))
		if err != nil {
			return nil, err
		}
		return u, nil
	case 3:
		return nil, fmt.Errorf("expected host:port:user:pass (got host:port:user only)")
	case 4:
		host, port, user, pass := parts[0], parts[1], parts[2], parts[3]
		if pass == "" {
			return nil, fmt.Errorf("empty proxy password")
		}
		u, err := url.Parse(fmt.Sprintf("http://%s:%s", host, port))
		if err != nil {
			return nil, err
		}
		u.User = url.UserPassword(user, pass)
		return u, nil
	default:
		return nil, fmt.Errorf("expected host:port or host:port:user:pass")
	}
}

func (p *ProxyPool) Next() *url.URL {
	return p.NextAvailable()
}

// NextAvailable returns the next proxy not in cooldown (or the soonest-ready one).
func (p *ProxyPool) NextAvailable() *url.URL {
	if p == nil || len(p.proxies) == 0 {
		return nil
	}
	n := len(p.proxies)
	now := time.Now().UnixNano()
	start := int(p.idx.Add(1)-1) % n
	bestIdx := -1
	var bestUntil int64
	for i := 0; i < n; i++ {
		idx := (start + i) % n
		until := p.cooldownUntil[idx].Load()
		if until <= now {
			return p.proxies[idx]
		}
		if bestIdx < 0 || until < bestUntil {
			bestIdx = idx
			bestUntil = until
		}
	}
	return p.proxies[bestIdx]
}

func (p *ProxyPool) MarkCooldown(u *url.URL, d time.Duration) {
	if p == nil || u == nil || d <= 0 {
		return
	}
	idx := p.indexOf(u)
	if idx < 0 {
		return
	}
	until := time.Now().Add(d).UnixNano()
	for {
		cur := p.cooldownUntil[idx].Load()
		if cur >= until {
			return
		}
		if p.cooldownUntil[idx].CompareAndSwap(cur, until) {
			return
		}
	}
}

func (p *ProxyPool) indexOf(u *url.URL) int {
	for i, pr := range p.proxies {
		if proxyEqual(pr, u) {
			return i
		}
	}
	return -1
}

func proxyEqual(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	ap, _ := a.User.Password()
	bp, _ := b.User.Password()
	return a.Host == b.Host && ap == bp
}

func parseWaitSeconds(reason string) int {
	m := waitSecondsRE.FindStringSubmatch(reason)
	if len(m) < 2 {
		return 0
	}
	n := 0
	for _, c := range m[1] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func cooldownForRateReason(reason string) time.Duration {
	if sec := parseWaitSeconds(reason); sec > 0 {
		return time.Duration(sec)*time.Second + 3*time.Second
	}
	return 90 * time.Second
}

func (p *ProxyPool) Len() int {
	if p == nil {
		return 0
	}
	return len(p.proxies)
}

// MaxInflight — one concurrent login per sticky session (avoids same-IP rate limits).
func (p *ProxyPool) MaxInflight() int {
	if p == nil || len(p.proxies) == 0 {
		return 0
	}
	if len(p.proxies) == 1 {
		return 12
	}
	n := len(p.proxies)
	if n > 50 {
		n = 50
	}
	return n
}

func (p *ProxyPool) SuggestedWorkers() int {
	return p.MaxInflight()
}

func proxyDisplay(u *url.URL) string {
	if u == nil {
		return "direct"
	}
	host := u.Host
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			return fmt.Sprintf("%s:***@%s", name, host)
		}
	}
	return host
}
