package report

import (
	"fmt"
	"time"

	"ebs-netwatch/internal/monitor"
)

type StatusBlock struct {
	ID        string            `json:"id"`
	Title     string            `json:"title"`
	Level     string            `json:"level"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	UpdatedAt time.Time         `json:"updatedAt"`
}

type DashboardReport struct {
	GeneratedAt time.Time        `json:"generatedAt"`
	Blocks      []StatusBlock    `json:"blocks"`
	Snapshot    monitor.Snapshot `json:"snapshot"`
}

func Build(snapshot monitor.Snapshot) DashboardReport {
	return DashboardReport{
		GeneratedAt: time.Now().UTC(),
		Blocks: []StatusBlock{
			overallBlock(snapshot),
			networkBlock(snapshot),
			endpointsBlock(snapshot),
			diagnosticsBlock(snapshot),
		},
		Snapshot: snapshot,
	}
}

func overallBlock(snapshot monitor.Snapshot) StatusBlock {
	return StatusBlock{
		ID:      "overall",
		Title:   "Overall Status",
		Level:   string(snapshot.Status),
		Message: statusMessage(snapshot.Status),
		Fields: map[string]string{
			"healthy_endpoints": fmt.Sprintf("%d", healthyCount(snapshot.Endpoints)),
			"total_endpoints":   fmt.Sprintf("%d", len(snapshot.Endpoints)),
		},
		UpdatedAt: snapshot.UpdatedAt,
	}
}

func networkBlock(snapshot monitor.Snapshot) StatusBlock {
	fields := map[string]string{
		"default_interface": snapshot.Interface.DefaultInterface,
		"connection_type":   snapshot.Interface.ConnectionType,
		"gateway":           snapshot.Interface.Gateway,
	}
	if snapshot.Interface.LinkSpeedMbps > 0 {
		fields["link_speed_mbps"] = fmt.Sprintf("%d", snapshot.Interface.LinkSpeedMbps)
	} else {
		fields["link_speed_mbps"] = "n/a"
	}
	return StatusBlock{
		ID:        "network",
		Title:     "Network Path",
		Level:     "info",
		Message:   "Local interface and route summary.",
		Fields:    fields,
		UpdatedAt: snapshot.UpdatedAt,
	}
}

func endpointsBlock(snapshot monitor.Snapshot) StatusBlock {
	healthy := healthyCount(snapshot.Endpoints)
	return StatusBlock{
		ID:      "endpoints",
		Title:   "Endpoint Checks",
		Level:   endpointLevel(snapshot.Status),
		Message: fmt.Sprintf("%d of %d endpoints are healthy.", healthy, len(snapshot.Endpoints)),
		Fields: map[string]string{
			"healthy": fmt.Sprintf("%d", healthy),
			"total":   fmt.Sprintf("%d", len(snapshot.Endpoints)),
		},
		UpdatedAt: snapshot.UpdatedAt,
	}
}

func diagnosticsBlock(snapshot monitor.Snapshot) StatusBlock {
	if snapshot.Diagnostics == nil {
		return StatusBlock{
			ID:        "diagnostics",
			Title:     "Diagnostics",
			Level:     "info",
			Message:   "No diagnostics were needed in the latest cycle.",
			UpdatedAt: snapshot.UpdatedAt,
		}
	}

	fields := map[string]string{
		"default_interface": snapshot.Diagnostics.DefaultInterface,
		"connection_type":   snapshot.Diagnostics.ConnectionType,
		"gateway":           snapshot.Diagnostics.Gateway,
		"gateway_probe":     snapshot.Diagnostics.GatewayProbe,
		"gateway_reachable": fmt.Sprintf("%t", snapshot.Diagnostics.GatewayReachable),
	}
	if snapshot.Diagnostics.LinkSpeedMbps > 0 {
		fields["link_speed_mbps"] = fmt.Sprintf("%d", snapshot.Diagnostics.LinkSpeedMbps)
	}
	for i, lookup := range snapshot.Diagnostics.DNSLookups {
		key := fmt.Sprintf("dns_%d", i+1)
		if lookup.Error != "" {
			fields[key] = lookup.Host + ": " + lookup.Error
			continue
		}
		fields[key] = lookup.Host + ": " + fmt.Sprintf("%d addresses", len(lookup.Addresses))
	}

	return StatusBlock{
		ID:        "diagnostics",
		Title:     "Diagnostics",
		Level:     "warn",
		Message:   "Gateway and DNS checks ran because one or more endpoints were abnormal.",
		Fields:    fields,
		UpdatedAt: snapshot.UpdatedAt,
	}
}

func statusMessage(status monitor.Status) string {
	switch status {
	case monitor.StatusOperational:
		return "All checks are behaving normally."
	case monitor.StatusDegraded:
		return "One endpoint is unhealthy, but availability is still mostly intact."
	case monitor.StatusMajorOutageCandidate:
		return "Two or more endpoints are unhealthy."
	default:
		return "Status is unknown."
	}
}

func endpointLevel(status monitor.Status) string {
	switch status {
	case monitor.StatusOperational:
		return "info"
	case monitor.StatusDegraded:
		return "warn"
	case monitor.StatusMajorOutageCandidate:
		return "critical"
	default:
		return "info"
	}
}

func healthyCount(endpoints []monitor.EndpointResult) int {
	count := 0
	for _, endpoint := range endpoints {
		if endpoint.OK {
			count++
		}
	}
	return count
}
