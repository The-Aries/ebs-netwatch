package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Endpoint struct {
	Name           string         `json:"name"`
	URL            string         `json:"url"`
	ExpectedStatus ExpectedStatus `json:"expectedStatus"`
}

type ExpectedStatus struct {
	Exact *int `json:"exact,omitempty"`
	Min   int  `json:"min,omitempty"`
	Max   int  `json:"max,omitempty"`
}

type Config struct {
	DashboardAddress       string     `json:"dashboardAddress"`
	DataDir                string     `json:"dataDir"`
	RawRetentionDays       int        `json:"rawRetentionDays"`
	NormalIntervalSeconds  int        `json:"normalIntervalSeconds"`
	SuspectIntervalSeconds int        `json:"suspectIntervalSeconds"`
	MajorOutageThreshold   int        `json:"majorOutageThreshold"`
	RecoveryThreshold      int        `json:"recoveryThreshold"`
	HTTPTimeoutSeconds     int        `json:"httpTimeoutSeconds"`
	Endpoints              []Endpoint `json:"endpoints"`
}

func Default() Config {
	return Config{
		DashboardAddress:       "127.0.0.1:8080",
		DataDir:                "data",
		RawRetentionDays:       14,
		NormalIntervalSeconds:  30,
		SuspectIntervalSeconds: 10,
		MajorOutageThreshold:   3,
		RecoveryThreshold:      3,
		HTTPTimeoutSeconds:     5,
		Endpoints: []Endpoint{
			{
				Name: "google_204",
				URL:  "https://www.google.com/generate_204",
				ExpectedStatus: ExpectedStatus{
					Exact: intPtr(204),
				},
			},
			{
				Name: "cloudflare_trace",
				URL:  "https://www.cloudflare.com/cdn-cgi/trace",
				ExpectedStatus: ExpectedStatus{
					Min: 200,
					Max: 399,
				},
			},
			{
				Name: "gstatic_204",
				URL:  "https://www.gstatic.com/generate_204",
				ExpectedStatus: ExpectedStatus{
					Exact: intPtr(204),
				},
			},
		},
	}
}

func Load(path string) (Config, error) {
	cfg := Default()

	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}

	applyDefaults(&cfg)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (e ExpectedStatus) Matches(code int) bool {
	if e.Exact != nil {
		return code == *e.Exact
	}
	if e.Min == 0 && e.Max == 0 {
		return code >= 200 && code < 400
	}
	return code >= e.Min && code <= e.Max
}

func (e ExpectedStatus) String() string {
	if e.Exact != nil {
		return fmt.Sprintf("%d", *e.Exact)
	}
	if e.Min == 0 && e.Max == 0 {
		return "200-399"
	}
	return fmt.Sprintf("%d-%d", e.Min, e.Max)
}

func applyDefaults(cfg *Config) {
	if strings.TrimSpace(cfg.DashboardAddress) == "" {
		cfg.DashboardAddress = Default().DashboardAddress
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		cfg.DataDir = Default().DataDir
	}
	if cfg.RawRetentionDays <= 0 {
		cfg.RawRetentionDays = Default().RawRetentionDays
	}
	if cfg.NormalIntervalSeconds <= 0 {
		cfg.NormalIntervalSeconds = Default().NormalIntervalSeconds
	}
	if cfg.SuspectIntervalSeconds <= 0 {
		cfg.SuspectIntervalSeconds = Default().SuspectIntervalSeconds
	}
	if cfg.MajorOutageThreshold <= 0 {
		cfg.MajorOutageThreshold = Default().MajorOutageThreshold
	}
	if cfg.RecoveryThreshold <= 0 {
		cfg.RecoveryThreshold = Default().RecoveryThreshold
	}
	if cfg.HTTPTimeoutSeconds <= 0 {
		cfg.HTTPTimeoutSeconds = Default().HTTPTimeoutSeconds
	}
	if len(cfg.Endpoints) == 0 {
		cfg.Endpoints = Default().Endpoints
	}
}

func validate(cfg Config) error {
	if len(cfg.Endpoints) == 0 {
		return fmt.Errorf("config must define at least one endpoint")
	}
	for i, endpoint := range cfg.Endpoints {
		if strings.TrimSpace(endpoint.Name) == "" {
			return fmt.Errorf("endpoint[%d] is missing name", i)
		}
		if strings.TrimSpace(endpoint.URL) == "" {
			return fmt.Errorf("endpoint[%d] is missing url", i)
		}
		if endpoint.ExpectedStatus.Exact == nil && endpoint.ExpectedStatus.Min == 0 && endpoint.ExpectedStatus.Max == 0 {
			return fmt.Errorf("endpoint[%d] must define expectedStatus", i)
		}
		if endpoint.ExpectedStatus.Min != 0 || endpoint.ExpectedStatus.Max != 0 {
			if endpoint.ExpectedStatus.Min <= 0 || endpoint.ExpectedStatus.Max <= 0 || endpoint.ExpectedStatus.Min > endpoint.ExpectedStatus.Max {
				return fmt.Errorf("endpoint[%d] has invalid expectedStatus range", i)
			}
		}
	}
	return nil
}

func intPtr(v int) *int {
	return &v
}
