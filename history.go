package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	historySchemaVersion = 9
	maxHistoryRecords    = 100
)

type HistoryRecord struct {
	SchemaVersion   int                        `json:"schema_version"`
	Kind            string                     `json:"kind"`
	HistoryID       string                     `json:"history_id"`
	CreatedAt       string                     `json:"created_at"`
	Result          DiagResult                 `json:"result"`
	Stability       *StabilityResult           `json:"stability,omitempty"`
	SitePatrol      *SitePatrolResult          `json:"site_patrol,omitempty"`
	CDNOptimize     *CDNOptimizeResult         `json:"cdn_optimize,omitempty"`
	NetworkBaseline *NetworkBaselineResult     `json:"network_baseline,omitempty"`
	PathCompare     *PathCompareResult         `json:"path_compare,omitempty"`
	DNSDiagnosis    *DNSDiagnoseResult         `json:"dns_diagnosis,omitempty"`
	PortTest        *PortTestResult            `json:"port_test,omitempty"`
	Integrated      *IntegratedDiagnosisResult `json:"integrated,omitempty"`
	Traceroute      *TraceResult               `json:"traceroute,omitempty"`
	Environment     NetworkEnvironment         `json:"environment"`
}

type historyDocument struct {
	SchemaVersion int             `json:"schema_version"`
	Records       []HistoryRecord `json:"records"`
}

type historyStore struct {
	path string
	mu   sync.Mutex
}

func (a *App) GetHistory() ([]HistoryRecord, error) {
	store, err := a.getHistoryStore()
	if err != nil {
		return nil, err
	}
	return store.list()
}

func (a *App) SaveHistory(record HistoryRecord) error {
	store, err := a.getHistoryStore()
	if err != nil {
		return err
	}
	return store.save(record)
}

func (a *App) ClearHistory() error {
	store, err := a.getHistoryStore()
	if err != nil {
		return err
	}
	return store.replace(nil)
}

// MigrateLegacyHistory imports the previous flattened localStorage payload.
// Existing record IDs win, so the migration can safely be retried.
func (a *App) MigrateLegacyHistory(rawJSON string) (int, error) {
	var legacy []struct {
		HistoryID  string       `json:"history_id"`
		Traceroute *TraceResult `json:"traceroute,omitempty"`
		DiagResult
	}
	if err := json.Unmarshal([]byte(rawJSON), &legacy); err != nil {
		return 0, fmt.Errorf("decode legacy history: %w", err)
	}
	store, err := a.getHistoryStore()
	if err != nil {
		return 0, err
	}
	records := make([]HistoryRecord, 0, len(legacy))
	for _, item := range legacy {
		if strings.TrimSpace(item.TargetURL) == "" || strings.TrimSpace(item.CheckedAt) == "" {
			continue
		}
		records = append(records, HistoryRecord{
			SchemaVersion: historySchemaVersion,
			Kind:          "diagnosis",
			HistoryID:     item.HistoryID,
			CreatedAt:     item.CheckedAt,
			Result:        item.DiagResult,
			Traceroute:    item.Traceroute,
			Environment: NetworkEnvironment{
				CapturedAt: item.CheckedAt,
				ProxyMode:  item.ProxyMode,
				ProxyURL:   item.ProxyURL,
			},
		})
	}
	return store.importRecords(records)
}

func (a *App) getHistoryStore() (*historyStore, error) {
	a.historyMu.Lock()
	defer a.historyMu.Unlock()
	if a.historyStore != nil {
		return a.historyStore, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user config directory: %w", err)
	}
	a.historyStore = &historyStore{path: filepath.Join(configDir, "ATCF-NET", "history.json")}
	return a.historyStore, nil
}

func (s *historyStore) list() ([]HistoryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readLocked()
}

func (s *historyStore) save(record HistoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.readLocked()
	if err != nil {
		return err
	}
	record = normaliseHistoryRecord(record)
	filtered := make([]HistoryRecord, 0, len(records)+1)
	filtered = append(filtered, record)
	for _, existing := range records {
		if existing.HistoryID != record.HistoryID {
			filtered = append(filtered, existing)
		}
	}
	return s.writeLocked(limitHistory(filtered))
}

func (s *historyStore) importRecords(imported []HistoryRecord) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.readLocked()
	if err != nil {
		return 0, err
	}
	known := make(map[string]struct{}, len(existing)+len(imported))
	for _, record := range existing {
		known[record.HistoryID] = struct{}{}
	}
	added := 0
	for _, record := range imported {
		record = normaliseHistoryRecord(record)
		if _, found := known[record.HistoryID]; found {
			continue
		}
		known[record.HistoryID] = struct{}{}
		existing = append(existing, record)
		added++
	}
	if added == 0 {
		return 0, nil
	}
	return added, s.writeLocked(limitHistory(existing))
}

func (s *historyStore) replace(records []HistoryRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(limitHistory(records))
}

func (s *historyStore) readLocked() ([]HistoryRecord, error) {
	contents, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	var document historyDocument
	if err := json.Unmarshal(contents, &document); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}
	for index := range document.Records {
		document.Records[index] = normaliseHistoryRecord(document.Records[index])
	}
	return limitHistory(document.Records), nil
}

func (s *historyStore) writeLocked(records []HistoryRecord) error {
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create history directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".history-*.tmp")
	if err != nil {
		return fmt.Errorf("create history file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure history file: %w", err)
	}
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	document := historyDocument{SchemaVersion: historySchemaVersion, Records: records}
	if err := encoder.Encode(document); err != nil {
		temporary.Close()
		return fmt.Errorf("encode history: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync history: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close history: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace history: %w", err)
	}
	return nil
}

func normaliseHistoryRecord(record HistoryRecord) HistoryRecord {
	record.SchemaVersion = historySchemaVersion
	if record.Kind != "stability" && record.Kind != "site_patrol" && record.Kind != "cdn_optimize" && record.Kind != "network_baseline" && record.Kind != "path_compare" && record.Kind != "dns_diagnosis" && record.Kind != "port_test" && record.Kind != "integrated" {
		record.Kind = "diagnosis"
	}
	record.HistoryID = strings.TrimSpace(record.HistoryID)
	if record.HistoryID == "" {
		record.HistoryID = newTaskID()
	}
	if record.CreatedAt == "" {
		record.CreatedAt = record.Result.CheckedAt
	}
	if record.CreatedAt == "" {
		record.CreatedAt = time.Now().Format(time.RFC3339)
	}
	return record
}

func limitHistory(records []HistoryRecord) []HistoryRecord {
	if len(records) <= maxHistoryRecords {
		return records
	}
	return records[:maxHistoryRecords]
}
