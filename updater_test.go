package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSemanticVersionComparison(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		newer     bool
	}{
		{"1.2.4", "1.2.3", true},
		{"v2.0.0", "1.9.9", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.2", "1.2.3", false},
		{"1.2.3", "1.2.3-beta.2", true},
		{"1.2.3-beta.2", "1.2.3-beta.1", true},
		{"invalid", "1.0.0", false},
	}
	for _, test := range tests {
		if got := newerSemanticVersion(test.candidate, test.current); got != test.newer {
			t.Errorf("newerSemanticVersion(%q, %q)=%v want %v", test.candidate, test.current, got, test.newer)
		}
	}
}

func TestExpectedUpdateAssetName(t *testing.T) {
	tests := map[string]string{
		"darwin/arm64":  "ATNetool_1.2.3_darwin_arm64.app.tar.gz",
		"darwin/amd64":  "ATNetool_1.2.3_darwin_amd64.app.tar.gz",
		"windows/amd64": "ATNetool_1.2.3_windows_amd64.zip",
		"linux/arm64":   "ATNetool_1.2.3_linux_arm64.tar.gz",
		"linux/amd64":   "ATNetool_1.2.3_linux_amd64.tar.gz",
	}
	for platform, want := range tests {
		operatingSystem, architecture, _ := strings.Cut(platform, "/")
		got, err := expectedUpdateAssetName("1.2.3", operatingSystem, architecture)
		if err != nil || got != want {
			t.Errorf("platform=%s got=%q err=%v want=%q", platform, got, err, want)
		}
	}
	if _, err := expectedUpdateAssetName("1.2.3", "freebsd", "amd64"); err == nil {
		t.Fatal("unsupported platform must return an error")
	}
}

func TestUpdateSignatureAndChecksumVerification(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	checksums := []byte("2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824  update.tar.gz\n")
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums))
	if err = verifyChecksumsSignature(publicKey, checksums, signature); err != nil {
		t.Fatal(err)
	}
	if err = verifyChecksumsSignature(publicKey, append([]byte(nil), checksums[:len(checksums)-1]...), signature); err == nil {
		t.Fatal("tampered checksums must fail signature verification")
	}
	hash, ok := lookupExpectedChecksum(string(checksums), "update.tar.gz")
	if !ok || hash != strings.Fields(string(checksums))[0] {
		t.Fatalf("hash=%q ok=%v", hash, ok)
	}
	if _, ok = lookupExpectedChecksum(string(checksums), "other.tar.gz"); ok {
		t.Fatal("checksum lookup must use the exact filename")
	}
}

func TestParseUpdateVerifyKey(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	parsed, err := parseUpdateVerifyKey(encoded)
	if err != nil || !publicKey.Equal(parsed) {
		t.Fatalf("parsed=%v err=%v", parsed, err)
	}
}

func TestUpdateManagerChecksDownloadsAndVerifiesRelease(t *testing.T) {
	assetName, err := expectedUpdateAssetName("1.1.0", runtime.GOOS, runtime.GOARCH)
	if err != nil {
		t.Skip(err)
	}
	asset := []byte("signed update archive")
	digest := sha256.Sum256(asset)
	checksums := []byte(hex.EncodeToString(digest[:]) + "  " + assetName + "\n")
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums))

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/latest":
			release := githubUpdateRelease{
				TagName: "v1.1.0",
				Assets: []githubUpdateAsset{
					{Name: assetName, BrowserDownloadURL: server.URL + "/asset", Size: int64(len(asset))},
					{Name: "SHA256SUMS", BrowserDownloadURL: server.URL + "/sums"},
					{Name: "SHA256SUMS.sig", BrowserDownloadURL: server.URL + "/signature"},
				},
			}
			_ = json.NewEncoder(writer).Encode(release)
		case "/asset":
			_, _ = writer.Write(asset)
		case "/sums":
			_, _ = writer.Write(checksums)
		case "/signature":
			_, _ = writer.Write([]byte(signature))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	manager := &updateManager{
		client:           server.Client(),
		latestReleaseURL: server.URL + "/latest",
		stageDirectory:   t.TempDir(),
		verifyKey:        publicKey,
		state: UpdateState{
			Phase: updatePhaseIdle, CurrentVersion: "1.0.0", Enabled: true,
		},
	}
	state, err := manager.Check(context.Background())
	if err != nil || state.Phase != updatePhaseAvailable || state.Release == nil {
		t.Fatalf("check state=%+v err=%v", state, err)
	}
	state, err = manager.Download(context.Background())
	if err != nil || state.Phase != updatePhaseReady || state.Progress != 100 {
		t.Fatalf("download state=%+v err=%v", state, err)
	}
	downloaded, err := os.ReadFile(filepath.Join(manager.stageDirectory, assetName))
	if err != nil || string(downloaded) != string(asset) {
		t.Fatalf("downloaded=%q err=%v", downloaded, err)
	}
}

func TestUpdateDownloadURLCandidates(t *testing.T) {
	original := "https://github.com/attson/atnetool/releases/download/v1.0.0/file.zip"
	candidates := updateDownloadURLCandidates(original)
	if len(candidates) < 2 || candidates[len(candidates)-1] != original {
		t.Fatalf("candidates=%v", candidates)
	}
	plain := "https://example.com/file.zip"
	if got := updateDownloadURLCandidates(plain); len(got) != 1 || got[0] != plain {
		t.Fatalf("plain candidates=%v", got)
	}
}

func TestFetchExpectedChecksumFallsBackAfterInvalidMirrorResponse(t *testing.T) {
	assetName := "ATNetool_1.2.3_linux_amd64.tar.gz"
	asset := []byte("release archive")
	digest := sha256.Sum256(asset)
	checksums := []byte(hex.EncodeToString(digest[:]) + "  " + assetName + "\n")
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, checksums))

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasPrefix(request.URL.Path, "/bad/") {
			_, _ = writer.Write([]byte("<html>temporary mirror page</html>"))
			return
		}
		switch {
		case strings.HasSuffix(request.URL.Path, "/SHA256SUMS.sig"):
			_, _ = writer.Write([]byte(signature))
		case strings.HasSuffix(request.URL.Path, "/SHA256SUMS"):
			_, _ = writer.Write(checksums)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	t.Setenv("ATNETOOL_UPDATE_DOWNLOAD_MIRRORS", server.URL+"/bad/{url},"+server.URL+"/good/{url}")

	manager := &updateManager{client: server.Client(), verifyKey: publicKey}
	hash, err := manager.fetchExpectedChecksum(context.Background(), updateDownloadSource{
		checksumURL:  "https://github.com/attson/atnetool/releases/download/v1.2.3/SHA256SUMS",
		signatureURL: "https://github.com/attson/atnetool/releases/download/v1.2.3/SHA256SUMS.sig",
	}, assetName)
	if err != nil {
		t.Fatal(err)
	}
	if hash != hex.EncodeToString(digest[:]) {
		t.Fatalf("hash=%q", hash)
	}
}

func TestCanceledDownloadCannotBecomeReady(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manager := &updateManager{
		state: UpdateState{
			Phase:   updatePhaseVerifying,
			Enabled: true,
			Release: &UpdateRelease{Version: "1.2.3"},
		},
	}
	state, err := manager.markReady(ctx, UpdateRelease{Version: "1.2.3", AssetSize: 10}, "/tmp/update")
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != updatePhaseAvailable || manager.stagedPath != "" {
		t.Fatalf("canceled download state=%+v staged=%q", state, manager.stagedPath)
	}
}
