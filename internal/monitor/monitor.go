package monitor

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"ebs-netwatch/internal/config"
	"ebs-netwatch/internal/network"
	"ebs-netwatch/internal/storage"
)

type Status string

const (
	StatusOperational          Status = "operational"
	StatusDegraded             Status = "degraded"
	StatusMajorOutageCandidate Status = "major_outage_candidate"
)

type EndpointResult struct {
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	Expected     string    `json:"expected"`
	OK           bool      `json:"ok"`
	StatusCode   *int      `json:"statusCode,omitempty"`
	FailurePhase string    `json:"failurePhase,omitempty"`
	Error        string    `json:"error,omitempty"`
	DurationMS   int64     `json:"durationMs"`
	CheckedAt    time.Time `json:"checkedAt"`
}

type DNSLookupResult struct {
	Host      string   `json:"host"`
	Addresses []string `json:"addresses,omitempty"`
	Error     string   `json:"error,omitempty"`
}

type DiagnosticResult struct {
	DefaultInterface string            `json:"defaultInterface"`
	ConnectionType   string            `json:"connectionType"`
	LinkSpeedMbps    int               `json:"linkSpeedMbps,omitempty"`
	Gateway          string            `json:"gateway,omitempty"`
	GatewayProbe     string            `json:"gatewayProbe,omitempty"`
	GatewayReachable bool              `json:"gatewayReachable"`
	DNSLookups       []DNSLookupResult `json:"dnsLookups,omitempty"`
	CheckedAt        time.Time         `json:"checkedAt"`
}

type CycleResult struct {
	CheckedAt   time.Time         `json:"checkedAt"`
	Status      Status            `json:"status"`
	Interface   network.Info      `json:"interface"`
	Endpoints   []EndpointResult  `json:"endpoints"`
	Diagnostics *DiagnosticResult `json:"diagnostics,omitempty"`
}

type Snapshot struct {
	UpdatedAt   time.Time         `json:"updatedAt"`
	Status      Status            `json:"status"`
	Interface   network.Info      `json:"interface"`
	Endpoints   []EndpointResult  `json:"endpoints"`
	Diagnostics *DiagnosticResult `json:"diagnostics,omitempty"`
	Recent      []CycleResult     `json:"recent"`
}

type Runner struct {
	cfg      config.Config
	linkInfo network.Info
	client   *http.Client
	appender *storage.Appender

	mu       sync.RWMutex
	status   Status
	snapshot Snapshot
	recent   []CycleResult
}

func NewRunner(cfg config.Config, linkInfo network.Info, appender *storage.Appender, recent []CycleResult) *Runner {
	timeout := time.Duration(cfg.HTTPTimeoutSeconds) * time.Second
	seededRecent := cloneCycles(recent)
	if len(seededRecent) > 20 {
		seededRecent = seededRecent[len(seededRecent)-20:]
	}

	status := StatusOperational
	snapshot := Snapshot{}
	if len(seededRecent) > 0 {
		last := seededRecent[len(seededRecent)-1]
		status = last.Status
		snapshot = Snapshot{
			UpdatedAt:   last.CheckedAt,
			Status:      last.Status,
			Interface:   last.Interface,
			Endpoints:   append([]EndpointResult(nil), last.Endpoints...),
			Diagnostics: cloneDiagnostic(last.Diagnostics),
			Recent:      cloneCycles(seededRecent),
		}
	}

	return &Runner{
		cfg:      cfg,
		linkInfo: linkInfo,
		client: &http.Client{
			Timeout: timeout,
		},
		appender: appender,
		status:   status,
		snapshot: snapshot,
		recent:   seededRecent,
	}
}

func (r *Runner) Run(ctx context.Context) error {
	if err := r.runOnce(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(r.currentInterval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.runOnce(ctx); err != nil {
				return err
			}
			ticker.Reset(r.currentInterval())
		}
	}
}

func (r *Runner) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneSnapshot(r.snapshot)
}

func (r *Runner) runOnce(ctx context.Context) error {
	endpoints := make([]EndpointResult, len(r.cfg.Endpoints))
	var wg sync.WaitGroup
	for i := range r.cfg.Endpoints {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			endpoints[index] = r.checkEndpoint(ctx, r.cfg.Endpoints[index])
		}(i)
	}
	wg.Wait()

	diagnostics := (*DiagnosticResult)(nil)
	if hasAbnormalEndpoint(endpoints) {
		diagnostics = r.runDiagnostics(ctx, endpoints)
	}

	status := r.nextStatus(endpoints)
	cycle := CycleResult{
		CheckedAt:   time.Now().UTC(),
		Status:      status,
		Interface:   r.linkInfo,
		Endpoints:   endpoints,
		Diagnostics: diagnostics,
	}

	r.mu.Lock()
	r.status = status
	r.recent = append(r.recent, cycle)
	if len(r.recent) > 20 {
		r.recent = r.recent[len(r.recent)-20:]
	}
	r.snapshot = Snapshot{
		UpdatedAt:   cycle.CheckedAt,
		Status:      status,
		Interface:   r.linkInfo,
		Endpoints:   endpoints,
		Diagnostics: diagnostics,
		Recent:      cloneCycles(r.recent),
	}
	r.mu.Unlock()

	if r.appender != nil {
		_ = r.appender.Append(cycle)
	}
	return nil
}

func (r *Runner) currentInterval() time.Duration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.status == StatusOperational {
		return time.Duration(r.cfg.NormalIntervalSeconds) * time.Second
	}
	return time.Duration(r.cfg.SuspectIntervalSeconds) * time.Second
}

func (r *Runner) nextStatus(endpoints []EndpointResult) Status {
	if len(endpoints) == 0 {
		return StatusMajorOutageCandidate
	}

	okCount := 0
	for _, endpoint := range endpoints {
		if endpoint.OK {
			okCount++
		}
	}

	switch {
	case okCount == len(endpoints):
		return StatusOperational
	case okCount >= len(endpoints)-1:
		return StatusDegraded
	default:
		return StatusMajorOutageCandidate
	}
}

func (r *Runner) checkEndpoint(ctx context.Context, endpoint config.Endpoint) EndpointResult {
	start := time.Now()
	result := EndpointResult{
		Name:      endpoint.Name,
		URL:       endpoint.URL,
		Expected:  endpoint.ExpectedStatus.String(),
		CheckedAt: start.UTC(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.URL, nil)
	if err != nil {
		result.FailurePhase = "request"
		result.Error = err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}

	resp, err := r.client.Do(req)
	if err != nil {
		result.FailurePhase = "transport"
		result.Error = err.Error()
		result.DurationMS = time.Since(start).Milliseconds()
		return result
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	result.DurationMS = time.Since(start).Milliseconds()
	result.StatusCode = intPtr(resp.StatusCode)
	if endpoint.ExpectedStatus.Matches(resp.StatusCode) {
		result.OK = true
		return result
	}

	result.FailurePhase = "http"
	result.Error = fmt.Sprintf("unexpected HTTP status %d, expected %s", resp.StatusCode, result.Expected)
	return result
}

func (r *Runner) runDiagnostics(ctx context.Context, endpoints []EndpointResult) *DiagnosticResult {
	diag := &DiagnosticResult{
		DefaultInterface: r.linkInfo.DefaultInterface,
		ConnectionType:   r.linkInfo.ConnectionType,
		LinkSpeedMbps:    r.linkInfo.LinkSpeedMbps,
		Gateway:          r.linkInfo.Gateway,
		CheckedAt:        time.Now().UTC(),
	}
	if diag.Gateway != "" {
		diag.GatewayProbe = "tcp ports 80, 443, 53"
		diag.GatewayReachable = probeGateway(ctx, diag.Gateway)
	}

	hosts := abnormalHosts(endpoints)
	for _, host := range hosts {
		lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		addrs, err := net.DefaultResolver.LookupHost(lookupCtx, host)
		cancel()
		lookup := DNSLookupResult{Host: host}
		if err != nil {
			lookup.Error = err.Error()
		} else {
			lookup.Addresses = append([]string(nil), addrs...)
		}
		diag.DNSLookups = append(diag.DNSLookups, lookup)
	}
	return diag
}

func probeGateway(ctx context.Context, gateway string) bool {
	ports := []string{"80", "443", "53"}
	for _, port := range ports {
		dialCtx, cancel := context.WithTimeout(ctx, time.Second)
		conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", net.JoinHostPort(gateway, port))
		cancel()
		if err == nil {
			conn.Close()
			return true
		}
	}
	return false
}

func hasAbnormalEndpoint(endpoints []EndpointResult) bool {
	for _, endpoint := range endpoints {
		if !endpoint.OK {
			return true
		}
	}
	return false
}

func abnormalHosts(endpoints []EndpointResult) []string {
	seen := map[string]struct{}{}
	var hosts []string
	for _, endpoint := range endpoints {
		if endpoint.OK {
			continue
		}
		parsed, err := url.Parse(endpoint.URL)
		if err != nil {
			continue
		}
		host := parsed.Hostname()
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Endpoints = append([]EndpointResult(nil), snapshot.Endpoints...)
	snapshot.Recent = cloneCycles(snapshot.Recent)
	if snapshot.Diagnostics != nil {
		diag := *snapshot.Diagnostics
		diag.DNSLookups = append([]DNSLookupResult(nil), snapshot.Diagnostics.DNSLookups...)
		snapshot.Diagnostics = &diag
	}
	return snapshot
}

func cloneDiagnostic(diag *DiagnosticResult) *DiagnosticResult {
	if diag == nil {
		return nil
	}
	copyDiag := *diag
	copyDiag.DNSLookups = append([]DNSLookupResult(nil), diag.DNSLookups...)
	return &copyDiag
}

func cloneCycles(cycles []CycleResult) []CycleResult {
	out := make([]CycleResult, len(cycles))
	for i, cycle := range cycles {
		out[i] = cycle
		out[i].Endpoints = append([]EndpointResult(nil), cycle.Endpoints...)
		if cycle.Diagnostics != nil {
			diag := *cycle.Diagnostics
			diag.DNSLookups = append([]DNSLookupResult(nil), cycle.Diagnostics.DNSLookups...)
			out[i].Diagnostics = &diag
		}
	}
	return out
}

func intPtr(v int) *int {
	return &v
}
