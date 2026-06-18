package config

import "time"

const CurrentVersion = 1

type File struct {
	Version  int      `yaml:"version"`
	Defaults Defaults `yaml:"defaults"`
	Jobs     Jobs     `yaml:"jobs"`
}

type Defaults struct {
	WorkingDir      string            `yaml:"working_dir"`
	Env             map[string]string `yaml:"env"`
	Timezone        string            `yaml:"timezone"`
	ThrottleSeconds int               `yaml:"throttle_seconds"`
	CatchUp         string            `yaml:"catch_up"`
	CircuitBreaker  CircuitBreaker    `yaml:"circuit_breaker"`
}

type Jobs map[string]Job

type Job struct {
	Type            string            `yaml:"type"`
	Command         []string          `yaml:"command"`
	WorkingDir      string            `yaml:"working_dir"`
	Env             map[string]string `yaml:"env"`
	Enabled         *bool             `yaml:"enabled"`
	Restart         string            `yaml:"restart"`
	ThrottleSeconds int               `yaml:"throttle_seconds"`
	CatchUp         string            `yaml:"catch_up"`
	Schedule        Schedule          `yaml:"schedule"`
	CircuitBreaker  CircuitBreaker    `yaml:"circuit_breaker"`
}

type Schedule struct {
	Cron     string `yaml:"cron"`
	Timezone string `yaml:"timezone"`
}

type CircuitBreaker struct {
	MaxFailures     int `yaml:"max_failures"`
	WindowSeconds   int `yaml:"window_seconds"`
	CooldownSeconds int `yaml:"cooldown_seconds"`
}

type ResolvedJob struct {
	ID             string
	Type           string
	Command        []string
	WorkingDir     string
	Env            map[string]string
	Enabled        bool
	Restart        string
	Schedule       Schedule
	Throttle       time.Duration
	CatchUp        time.Duration // 0 means strict (no catch-up beyond the scheduled minute)
	CircuitBreaker CircuitBreaker
}
