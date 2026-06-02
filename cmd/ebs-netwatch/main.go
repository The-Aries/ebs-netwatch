package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"ebs-netwatch/internal/config"
	"ebs-netwatch/internal/monitor"
	"ebs-netwatch/internal/network"
	"ebs-netwatch/internal/server"
	"ebs-netwatch/internal/storage"
)

const recentHistoryWindow = 24 * time.Hour
const rawLogMaintenanceInterval = time.Hour

var preparePublish = flag.Bool("prepare-publish", false, "refresh raw log retention and manifest, then exit")
var bindOverride = flag.String("bind", "", "override the dashboard bind address (address:port)")

func main() {
	flag.Parse()
	if *preparePublish {
		if err := runPreparePublish(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if runtime.GOOS != "linux" {
		return errors.New("ebs-netwatch supports Linux only")
	}

	cfg, err := loadRuntimeConfig()
	if err != nil {
		return err
	}
	if trimmed := strings.TrimSpace(*bindOverride); trimmed != "" {
		cfg.DashboardAddress = trimmed
	}

	defaultInterface, gateway, err := network.DetectDefaultRoute()
	if err != nil {
		log.Printf("network detection warning: %v", err)
	}

	linkInfo := network.Info{
		DefaultInterface: defaultInterface,
		ConnectionType:   network.ConnectionType(defaultInterface),
		Gateway:          gateway,
	}
	if speed, err := network.LinkSpeedMbps(defaultInterface); err == nil {
		linkInfo.LinkSpeedMbps = speed
	}

	if err := maintainRawLogs(cfg); err != nil {
		return err
	}

	appender := storage.NewAppender(cfg.DataDir)
	recentCycles := loadRecentCycles(cfg.DataDir, recentHistoryWindow)
	runner := monitor.NewRunner(cfg, linkInfo, appender, recentCycles, recentHistoryWindow)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("monitor stopped with error: %v", err)
			cancel()
		}
	}()

	go func() {
		ticker := time.NewTicker(rawLogMaintenanceInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := maintainRawLogs(cfg); err != nil {
					log.Printf("raw log maintenance failed: %v", err)
				}
			}
		}
	}()

	srv := server.New(runner)
	fmt.Printf("Dashboard listening on %s\n", cfg.DashboardAddress)
	printStartupURLs(cfg.DashboardAddress)
	if err := srv.Run(ctx, cfg.DashboardAddress); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("dashboard server failed: %w", err)
	}
	return nil
}

func runPreparePublish() error {
	cfg, err := loadMaintenanceConfig()
	if err != nil {
		return err
	}
	return maintainRawLogs(cfg)
}

func loadRuntimeConfig() (config.Config, error) {
	return loadConfigWithFallback("config.json")
}

func loadMaintenanceConfig() (config.Config, error) {
	return loadConfigWithFallback("config.json")
}

func loadConfigWithFallback(path string) (config.Config, error) {
	if _, err := os.Stat(path); err == nil {
		return config.Load(path)
	} else if !os.IsNotExist(err) {
		return config.Config{}, err
	}
	return config.Default(), nil
}

func maintainRawLogs(cfg config.Config) error {
	manifest, removed, err := storage.MaintainRawLogs(cfg.DataDir, cfg.RawRetentionDays, time.Now())
	if err != nil {
		return err
	}
	for _, path := range removed {
		log.Printf("removed expired raw log: %s", path)
	}
	log.Printf("raw log manifest updated: %s (%d files)", storage.ManifestPath(cfg.DataDir), len(manifest.Files))
	return nil
}

func loadRecentCycles(dir string, window time.Duration) []monitor.CycleResult {
	files, err := storage.DailyRawLogFiles(dir)
	if err != nil {
		log.Printf("history scan warning: %v", err)
		return nil
	}

	var cycles []monitor.CycleResult
	for _, file := range files {
		cycles = append(cycles, loadCyclesFromPath(file.Path)...)
	}

	legacyCycles := loadCyclesFromPath(storage.LegacyLogPath(dir))
	cycles = append(legacyCycles, cycles...)
	sort.Slice(cycles, func(i, j int) bool {
		return cycles[i].CheckedAt.Before(cycles[j].CheckedAt)
	})

	if window > 0 {
		cutoff := time.Now().UTC().Add(-window)
		filtered := make([]monitor.CycleResult, 0, len(cycles))
		for _, cycle := range cycles {
			if cycle.CheckedAt.Before(cutoff) {
				continue
			}
			filtered = append(filtered, cycle)
		}
		cycles = filtered
	}
	return cycles
}

func loadCyclesFromPath(path string) []monitor.CycleResult {
	entries, err := storage.ReadJSONL(path, 0)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("history load warning: %v", err)
		}
		return nil
	}

	cycles := make([]monitor.CycleResult, 0, len(entries))
	for _, entry := range entries {
		var cycle monitor.CycleResult
		if err := json.Unmarshal(entry, &cycle); err != nil {
			log.Printf("history entry skipped: %v", err)
			continue
		}
		cycles = append(cycles, cycle)
	}
	sort.Slice(cycles, func(i, j int) bool {
		return cycles[i].CheckedAt.Before(cycles[j].CheckedAt)
	})
	return cycles
}

func printStartupURLs(bindAddress string) {
	host, port, err := net.SplitHostPort(bindAddress)
	if err != nil {
		fmt.Printf("Local URL: unavailable\n")
		fmt.Printf("LAN URL: not detected\n")
		return
	}

	fmt.Printf("Local URL: %s\n", localURLForHost(host, port))

	lanURLs := lanURLsForBind(host, port)
	if len(lanURLs) == 0 {
		fmt.Printf("LAN URL: not detected\n")
		return
	}
	for _, url := range lanURLs {
		fmt.Printf("LAN URL: %s\n", url)
	}
}

func localURLForHost(host, port string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return "http://127.0.0.1:" + port
	}

	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return "http://127.0.0.1:" + port
	}

	return "http://" + net.JoinHostPort(host, port)
}

func lanURLsForBind(host, port string) []string {
	switch host {
	case "", "0.0.0.0", "::":
		return detectLanURLs(port)
	}

	ip := net.ParseIP(host)
	if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsUnspecified() {
		return nil
	}
	if ip.IsPrivate() {
		return []string{"http://" + net.JoinHostPort(host, port)}
	}
	return nil
}

func detectLanURLs(port string) []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	urls := make([]string, 0, len(ifaces))
	seen := map[string]struct{}{}
	for _, iface := range ifaces {
		if !ifaceHasLanCandidate(iface) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipv4FromAddr(addr)
			if ip == nil || !ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() {
				continue
			}
			url := "http://" + net.JoinHostPort(ip.String(), port)
			if _, ok := seen[url]; ok {
				continue
			}
			seen[url] = struct{}{}
			urls = append(urls, url)
		}
	}
	sort.Strings(urls)
	return urls
}

func ifaceHasLanCandidate(iface net.Interface) bool {
	if iface.Flags&net.FlagUp == 0 {
		return false
	}
	if iface.Flags&net.FlagLoopback != 0 {
		return false
	}

	name := strings.ToLower(iface.Name)
	ignoredPrefixes := []string{
		"docker", "br", "veth", "virbr", "cni", "flannel", "tailscale", "zt",
		"wg", "tun", "tap", "vmnet", "vboxnet", "utun", "awdl", "llw",
		"podman", "kube", "sit", "gre", "dummy", "ip6tnl", "ip6gre",
	}
	for _, prefix := range ignoredPrefixes {
		if strings.HasPrefix(name, prefix) {
			return false
		}
	}
	return true
}

func ipv4FromAddr(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		if ip := value.IP.To4(); ip != nil {
			return ip
		}
	case *net.IPAddr:
		if ip := value.IP.To4(); ip != nil {
			return ip
		}
	}
	return nil
}
