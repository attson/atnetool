package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxReportArchiveBytes      = 16 << 20
	maxReportUncompressedBytes = 32 << 20
	maxReportFileBytes         = 16 << 20
	maxReportFileCount         = 16
)

// ImportedReportSummary is the small, trusted index shown before an imported
// report is saved to local history.
type ImportedReportSummary struct {
	FormatVersion int                    `json:"format_version"`
	Application   string                 `json:"application"`
	AppVersion    string                 `json:"app_version"`
	GeneratedAt   string                 `json:"generated_at"`
	HistoryID     string                 `json:"history_id"`
	Kind          string                 `json:"kind"`
	KindLabel     string                 `json:"kind_label"`
	CheckedAt     string                 `json:"checked_at"`
	Target        string                 `json:"target"`
	Success       bool                   `json:"success"`
	Conclusion    string                 `json:"conclusion"`
	Metrics       map[string]interface{} `json:"metrics,omitempty"`
	Proxy         map[string]interface{} `json:"proxy,omitempty"`
	Traceroute    map[string]interface{} `json:"traceroute,omitempty"`
}

// ImportedReport contains validated report data. Importing is deliberately
// read-only: the caller must explicitly pass Record to SaveHistory to keep it.
type ImportedReport struct {
	Filename string                `json:"filename"`
	Summary  ImportedReportSummary `json:"summary"`
	Record   HistoryRecord         `json:"record"`
	Files    []string              `json:"files"`
	Warnings []string              `json:"warnings,omitempty"`
}

// ImportDiagnosisReport asks the user for an ATNetool ZIP report and validates
// it locally. An empty result means the file picker was cancelled.
func (a *App) ImportDiagnosisReport() (ImportedReport, error) {
	selected, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "导入诊断报告包",
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "ZIP 诊断报告 (*.zip)", Pattern: "*.zip"},
		},
	})
	if err != nil || selected == "" {
		return ImportedReport{}, err
	}
	info, err := os.Stat(selected)
	if err != nil {
		return ImportedReport{}, fmt.Errorf("inspect diagnosis report: %w", err)
	}
	if !info.Mode().IsRegular() {
		return ImportedReport{}, errors.New("diagnosis report is not a regular file")
	}
	if info.Size() <= 0 || info.Size() > maxReportArchiveBytes {
		return ImportedReport{}, fmt.Errorf("diagnosis report size must be between 1 byte and %d MiB", maxReportArchiveBytes>>20)
	}
	contents, err := os.ReadFile(selected)
	if err != nil {
		return ImportedReport{}, fmt.Errorf("read diagnosis report: %w", err)
	}
	return parseDiagnosisReportBundle(contents, filepath.Base(selected))
}

func parseDiagnosisReportBundle(data []byte, filename string) (ImportedReport, error) {
	if len(data) == 0 || len(data) > maxReportArchiveBytes {
		return ImportedReport{}, fmt.Errorf("diagnosis report size must be between 1 byte and %d MiB", maxReportArchiveBytes>>20)
	}
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ImportedReport{}, fmt.Errorf("open diagnosis report: %w", err)
	}
	if len(archive.File) == 0 || len(archive.File) > maxReportFileCount {
		return ImportedReport{}, fmt.Errorf("diagnosis report must contain between 1 and %d files", maxReportFileCount)
	}

	files := make(map[string][]byte, len(archive.File))
	fileNames := make([]string, 0, len(archive.File))
	var declaredTotal uint64
	var actualTotal uint64
	for _, entry := range archive.File {
		name, validateErr := validateReportArchiveEntry(entry)
		if validateErr != nil {
			return ImportedReport{}, validateErr
		}
		if _, duplicate := files[name]; duplicate {
			return ImportedReport{}, fmt.Errorf("diagnosis report contains duplicate file %q", name)
		}
		if entry.UncompressedSize64 > maxReportFileBytes {
			return ImportedReport{}, fmt.Errorf("diagnosis report file %q exceeds %d MiB", name, maxReportFileBytes>>20)
		}
		declaredTotal += entry.UncompressedSize64
		if declaredTotal > maxReportUncompressedBytes {
			return ImportedReport{}, fmt.Errorf("diagnosis report expands beyond %d MiB", maxReportUncompressedBytes>>20)
		}
		contents, readErr := readLimitedReportFile(entry)
		if readErr != nil {
			return ImportedReport{}, fmt.Errorf("read diagnosis report file %q: %w", name, readErr)
		}
		actualTotal += uint64(len(contents))
		if actualTotal > maxReportUncompressedBytes {
			return ImportedReport{}, fmt.Errorf("diagnosis report expands beyond %d MiB", maxReportUncompressedBytes>>20)
		}
		files[name] = contents
		fileNames = append(fileNames, name)
	}

	summaryJSON, ok := files["summary.json"]
	if !ok {
		return ImportedReport{}, errors.New("diagnosis report is missing summary.json")
	}
	rawJSON, ok := files["evidence/raw-result.json"]
	if !ok {
		return ImportedReport{}, errors.New("diagnosis report is missing evidence/raw-result.json")
	}

	var summary ImportedReportSummary
	if err := json.Unmarshal(summaryJSON, &summary); err != nil {
		return ImportedReport{}, fmt.Errorf("decode summary.json: %w", err)
	}
	if summary.FormatVersion != reportFormatVersion {
		return ImportedReport{}, fmt.Errorf("unsupported diagnosis report format_version %d", summary.FormatVersion)
	}
	if summary.Application != "ATNetool" {
		return ImportedReport{}, fmt.Errorf("unsupported diagnosis report application %q", summary.Application)
	}

	var record HistoryRecord
	if err := json.Unmarshal(rawJSON, &record); err != nil {
		return ImportedReport{}, fmt.Errorf("decode evidence/raw-result.json: %w", err)
	}
	if record.SchemaVersion <= 0 || record.SchemaVersion > historySchemaVersion {
		return ImportedReport{}, fmt.Errorf("unsupported history schema_version %d", record.SchemaVersion)
	}
	if err := validateHistoryReportRecord(record); err != nil {
		return ImportedReport{}, fmt.Errorf("validate imported diagnosis evidence: %w", err)
	}
	if err := validateImportedReportConsistency(summary, record); err != nil {
		return ImportedReport{}, err
	}

	sort.Strings(fileNames)
	warnings := make([]string, 0, 2)
	if summary.AppVersion != "" && summary.AppVersion != appVersion {
		warnings = append(warnings, fmt.Sprintf("报告由 ATNetool %s 生成，当前版本为 %s", summary.AppVersion, appVersion))
	}
	if record.SchemaVersion < historySchemaVersion {
		warnings = append(warnings, fmt.Sprintf("历史记录格式将从 v%d 兼容读取为 v%d", record.SchemaVersion, historySchemaVersion))
	}
	return ImportedReport{
		Filename: strings.TrimSpace(filename),
		Summary:  summary,
		Record:   record,
		Files:    fileNames,
		Warnings: warnings,
	}, nil
}

func validateReportArchiveEntry(entry *zip.File) (string, error) {
	name := entry.Name
	if name == "" || strings.Contains(name, `\`) || strings.ContainsRune(name, '\x00') || path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") {
		return "", fmt.Errorf("diagnosis report contains unsafe path %q", name)
	}
	if entry.FileInfo().IsDir() || !entry.Mode().IsRegular() {
		return "", fmt.Errorf("diagnosis report contains unsupported entry %q", name)
	}
	if entry.Method != zip.Store && entry.Method != zip.Deflate {
		return "", fmt.Errorf("diagnosis report file %q uses unsupported compression", name)
	}
	if !isAllowedReportFilename(name) {
		return "", fmt.Errorf("diagnosis report contains unsupported file %q", name)
	}
	return name, nil
}

func isAllowedReportFilename(name string) bool {
	if name == "README.md" || name == "summary.json" {
		return true
	}
	if !strings.HasPrefix(name, "evidence/") || !strings.HasSuffix(name, ".json") {
		return false
	}
	base := strings.TrimSuffix(strings.TrimPrefix(name, "evidence/"), ".json")
	if base == "" || strings.Contains(base, "/") {
		return false
	}
	for _, character := range base {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func readLimitedReportFile(entry *zip.File) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	contents, err := io.ReadAll(io.LimitReader(reader, maxReportFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxReportFileBytes {
		return nil, fmt.Errorf("uncompressed data exceeds %d MiB", maxReportFileBytes>>20)
	}
	return contents, nil
}

func validateImportedReportConsistency(summary ImportedReportSummary, record HistoryRecord) error {
	checks := []struct {
		field    string
		index    string
		evidence string
	}{
		{"history_id", strings.TrimSpace(summary.HistoryID), strings.TrimSpace(record.HistoryID)},
		{"kind", summary.Kind, record.Kind},
		{"checked_at", summary.CheckedAt, record.CreatedAt},
		{"target", strings.TrimSpace(summary.Target), strings.TrimSpace(historyReportTarget(record))},
	}
	for _, check := range checks {
		if check.index == "" || check.evidence == "" || check.index != check.evidence {
			return fmt.Errorf("diagnosis report %s does not match raw evidence", check.field)
		}
	}
	if summary.Success != historyReportSuccess(record) {
		return errors.New("diagnosis report success does not match raw evidence")
	}
	return nil
}
