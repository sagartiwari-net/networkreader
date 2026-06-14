package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Settings    Settings          `json:"settings"`
	UserAgent   string            `json:"user_agent"`
	Variables   map[string]string `json:"variables"`
	Steps       []Step            `json:"steps"`
	PlanLabels  map[string]PlanLabel `json:"plan_labels"`
	Output      OutputConfig      `json:"output"`
}

type Settings struct {
	Workers        int `json:"workers"`
	TimeoutSeconds int `json:"timeout_seconds"`
	RetryOnError   int `json:"retry_on_error"`
}

type Step struct {
	ID             string                       `json:"id"`
	Name           string                       `json:"name"`
	Optional       bool                         `json:"optional"`
	Method         string                       `json:"method"`
	URL            string                       `json:"url"`
	Headers        map[string]string            `json:"headers"`
	Body           string                       `json:"body"`
	SuccessStatus  []int                        `json:"success_status"`
	Extract        map[string]ExtractRule       `json:"extract"`
	FailureSignals FailureSignals               `json:"failure_signals"`
}

type ExtractRule struct {
	Source    string `json:"source"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Transform string `json:"transform"`
}

type FailureSignals struct {
	StatusCodes  []int    `json:"status_codes"`
	BodyContains []string `json:"body_contains"`
}

type PlanLabel struct {
	Match map[string]string `json:"match"`
	Label string            `json:"label"`
}

type OutputConfig struct {
	HitLine    string            `json:"hit_line"`
	ConsoleHit string            `json:"console_hit"`
	ConsoleFail string           `json:"console_fail"`
	Files      map[string]string `json:"files"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Settings.Workers <= 0 {
		cfg.Settings.Workers = 50
	}
	if cfg.Settings.TimeoutSeconds <= 0 {
		cfg.Settings.TimeoutSeconds = 30
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
	}
	return &cfg, nil
}

func (c *Config) BaseURL() string {
	if v := c.Variables["base_url"]; v != "" {
		return v
	}
	return "https://app.buzzsumo.com"
}

func (c *Config) LoginURL() string {
	if v := c.Variables["login_url"]; v != "" {
		return v
	}
	return c.BaseURL() + "/login"
}

func (c *Config) AccountQueryURL() string {
	if v := c.Variables["account_query_url"]; v != "" {
		return v
	}
	return c.BaseURL() + "/resource/account/query"
}

func (c *Config) SegmentTraitsURL() string {
	if v := c.Variables["segment_traits_url"]; v != "" {
		return v
	}
	return c.BaseURL() + "/users/segment-traits"
}
