package storage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

type Appender struct {
	dir string
	mu  sync.Mutex
}

type RawLogFile struct {
	Date string `json:"date"`
	Path string `json:"path"`
}

type RawLogManifest struct {
	GeneratedAt   time.Time    `json:"generatedAt"`
	RetentionDays int          `json:"retentionDays"`
	Files         []RawLogFile `json:"files"`
}

var rawLogNamePattern = regexp.MustCompile(`^checks-(\d{4}-\d{2}-\d{2})\.jsonl$`)

func NewAppender(dir string) *Appender {
	return &Appender{dir: dir}
}

func (a *Appender) Append(v any) error {
	if a == nil || strings.TrimSpace(a.dir) == "" {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if err := os.MkdirAll(a.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(DailyLogPath(a.dir, time.Now()), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(append(data, '\n'))
	return err
}

func ReadJSONL(path string, limit int) ([][]byte, error) {
	if path == "" {
		return nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var entries [][]byte
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		entries = append(entries, append([]byte(nil), line...))
		if limit > 0 && len(entries) > limit {
			entries = append([][]byte(nil), entries[len(entries)-limit:]...)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func DailyLogName(t time.Time) string {
	return fmt.Sprintf("checks-%s.jsonl", t.In(time.Local).Format("2006-01-02"))
}

func DailyLogPath(dir string, t time.Time) string {
	return filepath.Join(dir, DailyLogName(t))
}

func ManifestPath(dir string) string {
	return filepath.Join(dir, "manifest.json")
}

func LegacyLogPath(dir string) string {
	return filepath.Join(dir, "checks.jsonl")
}

func ReadRawLogManifest(path string) (RawLogManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return RawLogManifest{}, nil
		}
		return RawLogManifest{}, err
	}

	var manifest RawLogManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return RawLogManifest{}, err
	}
	return manifest, nil
}

func DailyRawLogFiles(dir string) ([]RawLogFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	files := make([]RawLogFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		date, ok := parseRawLogDate(entry.Name())
		if !ok {
			continue
		}
		files = append(files, RawLogFile{
			Date: date.Format("2006-01-02"),
			Path: filepath.ToSlash(filepath.Join(dir, entry.Name())),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Date < files[j].Date
	})
	return files, nil
}

func RetainDailyRawLogs(dir string, retentionDays int, now time.Time) ([]string, error) {
	if retentionDays <= 0 {
		return nil, nil
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	cutoff := startOfDay(now).AddDate(0, 0, -(retentionDays - 1))
	var removed []string

	for _, entry := range files {
		if entry.IsDir() {
			continue
		}
		date, ok := parseRawLogDate(entry.Name())
		if !ok {
			continue
		}
		if date.Before(cutoff) {
			path := filepath.Join(dir, entry.Name())
			if err := os.Remove(path); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return removed, err
			}
			removed = append(removed, path)
		}
	}

	sort.Strings(removed)
	return removed, nil
}

func WriteRawLogManifest(dir string, retentionDays int, now time.Time) (RawLogManifest, error) {
	files, err := retainedDailyRawLogs(dir, retentionDays, now)
	if err != nil {
		return RawLogManifest{}, err
	}

	manifest := RawLogManifest{
		GeneratedAt:   now.UTC(),
		RetentionDays: retentionDays,
		Files:         files,
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return RawLogManifest{}, err
	}

	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return RawLogManifest{}, err
	}
	if err := os.WriteFile(ManifestPath(dir), append(raw, '\n'), 0o644); err != nil {
		return RawLogManifest{}, err
	}
	return manifest, nil
}

func MaintainRawLogs(dir string, retentionDays int, now time.Time) (RawLogManifest, []string, error) {
	removed, err := RetainDailyRawLogs(dir, retentionDays, now)
	if err != nil {
		return RawLogManifest{}, removed, err
	}
	manifest, err := WriteRawLogManifest(dir, retentionDays, now)
	return manifest, removed, err
}

func retainedDailyRawLogs(dir string, retentionDays int, now time.Time) ([]RawLogFile, error) {
	files, err := DailyRawLogFiles(dir)
	if err != nil {
		return nil, err
	}
	if retentionDays <= 0 {
		return files, nil
	}

	cutoff := startOfDay(now).AddDate(0, 0, -(retentionDays - 1))
	out := make([]RawLogFile, 0, len(files))
	for _, file := range files {
		date, ok := parseRawLogDate(filepath.Base(file.Path))
		if !ok {
			continue
		}
		if date.Before(cutoff) {
			continue
		}
		out = append(out, file)
	}
	return out, nil
}

func parseRawLogDate(name string) (time.Time, bool) {
	matches := rawLogNamePattern.FindStringSubmatch(name)
	if len(matches) != 2 {
		return time.Time{}, false
	}
	date, err := time.ParseInLocation("2006-01-02", matches[1], time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return date, true
}

func startOfDay(t time.Time) time.Time {
	local := t.In(time.Local)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
}
