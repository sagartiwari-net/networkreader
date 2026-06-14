package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
)

type ProxyPool struct {
	proxies []*url.URL
	idx     atomic.Uint64
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
	return &ProxyPool{proxies: proxies}, nil
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
	if p == nil || len(p.proxies) == 0 {
		return nil
	}
	i := p.idx.Add(1) - 1
	return p.proxies[i%uint64(len(p.proxies))]
}

func (p *ProxyPool) Len() int {
	if p == nil {
		return 0
	}
	return len(p.proxies)
}

func (p *ProxyPool) MaxInflight() int {
	if p == nil || len(p.proxies) == 0 {
		return 0
	}
	if len(p.proxies) == 1 {
		return 12
	}
	n := len(p.proxies) * 4
	if n > 30 {
		n = 30
	}
	if n < 12 {
		n = 12
	}
	return n
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
