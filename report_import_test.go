package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

func TestParseDiagnosisReportBundle(t *testing.T) {
	record := diagnosisReportFixture()
	bundle, err := buildHistoryReportBundle(record)
	if err != nil {
		t.Fatalf("buildHistoryReportBundle: %v", err)
	}

	imported, err := parseDiagnosisReportBundle(bundle, "report.zip")
	if err != nil {
		t.Fatalf("parseDiagnosisReportBundle: %v", err)
	}
	if imported.Filename != "report.zip" || imported.Summary.FormatVersion != reportFormatVersion {
		t.Fatalf("unexpected import metadata: %#v", imported)
	}
	if imported.Record.HistoryID != record.HistoryID || imported.Record.Kind != record.Kind {
		t.Fatalf("unexpected imported record: %#v", imported.Record)
	}
	if len(imported.Files) < 4 {
		t.Fatalf("expected report evidence files, got %v", imported.Files)
	}
}

func TestParseDiagnosisReportBundleRejectsMissingRequiredFiles(t *testing.T) {
	valid := validReportFiles(t)
	for _, missing := range []string{"summary.json", "evidence/raw-result.json"} {
		t.Run(missing, func(t *testing.T) {
			files := cloneReportFiles(valid)
			delete(files, missing)
			_, err := parseDiagnosisReportBundle(writeReportArchiveForTest(t, files), "report.zip")
			if err == nil || !strings.Contains(err.Error(), "missing "+missing) {
				t.Fatalf("expected missing-file error, got %v", err)
			}
		})
	}
}

func TestParseDiagnosisReportBundleRejectsInvalidJSONAndUnknownVersion(t *testing.T) {
	t.Run("invalid summary JSON", func(t *testing.T) {
		files := validReportFiles(t)
		files["summary.json"] = []byte("{")
		_, err := parseDiagnosisReportBundle(writeReportArchiveForTest(t, files), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "decode summary.json") {
			t.Fatalf("expected JSON error, got %v", err)
		}
	})

	t.Run("unknown format version", func(t *testing.T) {
		files := validReportFiles(t)
		var summary map[string]interface{}
		if err := json.Unmarshal(files["summary.json"], &summary); err != nil {
			t.Fatal(err)
		}
		summary["format_version"] = reportFormatVersion + 1
		files["summary.json"], _ = json.Marshal(summary)
		_, err := parseDiagnosisReportBundle(writeReportArchiveForTest(t, files), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "unsupported diagnosis report format_version") {
			t.Fatalf("expected format error, got %v", err)
		}
	})

	t.Run("invalid raw result JSON", func(t *testing.T) {
		files := validReportFiles(t)
		files["evidence/raw-result.json"] = []byte("not-json")
		_, err := parseDiagnosisReportBundle(writeReportArchiveForTest(t, files), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "decode evidence/raw-result.json") {
			t.Fatalf("expected raw JSON error, got %v", err)
		}
	})
}

func TestParseDiagnosisReportBundleRejectsUnsafeEntries(t *testing.T) {
	tests := []string{
		"../summary.json",
		"/summary.json",
		`evidence\raw-result.json`,
		"evidence/../summary.json",
		"evidence/nested/raw-result.json",
		"payload.zip",
		"notes.txt",
	}
	for _, unsafeName := range tests {
		t.Run(strings.ReplaceAll(unsafeName, "/", "_"), func(t *testing.T) {
			files := validReportFiles(t)
			files[unsafeName] = []byte("{}")
			_, err := parseDiagnosisReportBundle(writeReportArchiveForTest(t, files), "report.zip")
			if err == nil {
				t.Fatalf("unsafe entry %q was accepted", unsafeName)
			}
		})
	}
}

func TestParseDiagnosisReportBundleRejectsDuplicateAndSpecialEntries(t *testing.T) {
	t.Run("duplicate", func(t *testing.T) {
		entries := []reportArchiveTestEntry{
			{name: "summary.json", data: []byte("{}")},
			{name: "summary.json", data: []byte("{}")},
		}
		_, err := parseDiagnosisReportBundle(writeReportArchiveEntriesForTest(t, entries), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("expected duplicate error, got %v", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		entries := []reportArchiveTestEntry{{name: "summary.json", data: []byte("target"), mode: fs.ModeSymlink | 0o777}}
		_, err := parseDiagnosisReportBundle(writeReportArchiveEntriesForTest(t, entries), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "unsupported entry") {
			t.Fatalf("expected special-entry error, got %v", err)
		}
	})
}

func TestParseDiagnosisReportBundleEnforcesResourceLimits(t *testing.T) {
	t.Run("archive bytes", func(t *testing.T) {
		_, err := parseDiagnosisReportBundle(make([]byte, maxReportArchiveBytes+1), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "size must be") {
			t.Fatalf("expected archive size error, got %v", err)
		}
	})

	t.Run("single file", func(t *testing.T) {
		entries := []reportArchiveTestEntry{{name: "evidence/raw-result.json", data: make([]byte, maxReportFileBytes+1)}}
		_, err := parseDiagnosisReportBundle(writeReportArchiveEntriesForTest(t, entries), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "exceeds") {
			t.Fatalf("expected single-file size error, got %v", err)
		}
	})

	t.Run("expanded total", func(t *testing.T) {
		chunk := make([]byte, 11<<20)
		entries := []reportArchiveTestEntry{
			{name: "evidence/one.json", data: chunk},
			{name: "evidence/two.json", data: chunk},
			{name: "evidence/three.json", data: chunk},
		}
		_, err := parseDiagnosisReportBundle(writeReportArchiveEntriesForTest(t, entries), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "expands beyond") {
			t.Fatalf("expected expanded-size error, got %v", err)
		}
	})

	t.Run("file count", func(t *testing.T) {
		entries := make([]reportArchiveTestEntry, 0, maxReportFileCount+1)
		for index := 0; index <= maxReportFileCount; index++ {
			entries = append(entries, reportArchiveTestEntry{name: fmt.Sprintf("evidence/%d.json", index), data: []byte("{}")})
		}
		_, err := parseDiagnosisReportBundle(writeReportArchiveEntriesForTest(t, entries), "report.zip")
		if err == nil || !strings.Contains(err.Error(), "must contain") {
			t.Fatalf("expected file-count error, got %v", err)
		}
	})
}

func TestParseDiagnosisReportBundleRejectsSummaryEvidenceMismatch(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value interface{}
	}{
		{"kind", "kind", "stability"},
		{"history ID", "history_id", "different-id"},
		{"target", "target", "https://different.example"},
		{"success", "success", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := validReportFiles(t)
			var summary map[string]interface{}
			if err := json.Unmarshal(files["summary.json"], &summary); err != nil {
				t.Fatal(err)
			}
			summary[test.field] = test.value
			files["summary.json"], _ = json.Marshal(summary)
			_, err := parseDiagnosisReportBundle(writeReportArchiveForTest(t, files), "report.zip")
			if err == nil || !strings.Contains(err.Error(), "does not match raw evidence") {
				t.Fatalf("expected consistency error, got %v", err)
			}
		})
	}
}

func TestParseDiagnosisReportBundleRejectsKindWithoutMatchingEvidence(t *testing.T) {
	files := validReportFiles(t)
	var record HistoryRecord
	if err := json.Unmarshal(files["evidence/raw-result.json"], &record); err != nil {
		t.Fatal(err)
	}
	record.Kind = "stability"
	files["evidence/raw-result.json"], _ = json.Marshal(record)
	var summary map[string]interface{}
	if err := json.Unmarshal(files["summary.json"], &summary); err != nil {
		t.Fatal(err)
	}
	summary["kind"] = "stability"
	files["summary.json"], _ = json.Marshal(summary)

	_, err := parseDiagnosisReportBundle(writeReportArchiveForTest(t, files), "report.zip")
	if err == nil || !strings.Contains(err.Error(), "stability evidence is missing") {
		t.Fatalf("expected type/evidence error, got %v", err)
	}
}

func TestParsingReportDoesNotWriteHistory(t *testing.T) {
	store := &historyStore{path: t.TempDir() + "/history.json"}
	bundle, err := buildHistoryReportBundle(diagnosisReportFixture())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseDiagnosisReportBundle(bundle, "report.zip"); err != nil {
		t.Fatal(err)
	}
	records, err := store.list()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("parsing unexpectedly wrote %d history records", len(records))
	}
}

type reportArchiveTestEntry struct {
	name string
	data []byte
	mode fs.FileMode
}

func validReportFiles(t *testing.T) map[string][]byte {
	t.Helper()
	bundle, err := buildHistoryReportBundle(diagnosisReportFixture())
	if err != nil {
		t.Fatal(err)
	}
	return readReportArchive(t, bundle)
}

func cloneReportFiles(input map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(input))
	for name, contents := range input {
		cloned[name] = append([]byte(nil), contents...)
	}
	return cloned
}

func writeReportArchiveForTest(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	entries := make([]reportArchiveTestEntry, 0, len(files))
	for name, contents := range files {
		entries = append(entries, reportArchiveTestEntry{name: name, data: contents})
	}
	return writeReportArchiveEntriesForTest(t, entries)
}

func writeReportArchiveEntriesForTest(t *testing.T, entries []reportArchiveTestEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
