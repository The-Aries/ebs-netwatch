package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"syscall"
	"time"

	"ebs-netwatch/internal/config"
	"ebs-netwatch/internal/monitor"
	"ebs-netwatch/internal/network"
	"ebs-netwatch/internal/server"
	"ebs-netwatch/internal/storage"
)

const recentHistoryLimit = 20
const rawLogMaintenanceInterval = time.Hour

var preparePublish = flag.Bool("prepare-publish", false, "refresh raw log retention and manifest, then exit")

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

	cfg, err := config.Load("config.json")
	if err != nil {
		return err
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
	recentCycles := loadRecentCycles(cfg.DataDir, recentHistoryLimit)
	runner := monitor.NewRunner(cfg, linkInfo, appender, recentCycles)

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
	log.Printf("dashboard listening on http://%s", cfg.DashboardAddress)
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

func loadMaintenanceConfig() (config.Config, error) {
	if _, err := os.Stat("config.json"); err == nil {
		return config.Load("config.json")
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

func loadRecentCycles(dir string, limit int) []monitor.CycleResult {
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

	if len(cycles) > limit {
		cycles = cycles[len(cycles)-limit:]
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
