package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	appVersion                  = "0.0.0-dev"
	updateRepository            = "attson/atnetool"
	updateVerifyPublicKeyBase64 string
)

const (
	updatePhaseDisabled    = "disabled"
	updatePhaseIdle        = "idle"
	updatePhaseChecking    = "checking"
	updatePhaseUpToDate    = "up_to_date"
	updatePhaseAvailable   = "available"
	updatePhaseDownloading = "downloading"
	updatePhaseVerifying   = "verifying"
	updatePhaseReady       = "ready"
	updatePhaseApplying    = "applying"
	updatePhaseError       = "error"

	updateEventName = "updater:state"
)

var defaultUpdateMirrors = []string{
	"https://gh-proxy.com/",
	"https://github.akams.cn/",
}

type UpdateRelease struct {
	Version     string `json:"version"`
	Notes       string `json:"notes,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	AssetName   string `json:"asset_name"`
	AssetSize   int64  `json:"asset_size"`
}

type UpdateState struct {
	Phase          string         `json:"phase"`
	CurrentVersion string         `json:"current_version"`
	Enabled        bool           `json:"enabled"`
	Release        *UpdateRelease `json:"release,omitempty"`
	Progress       int            `json:"progress"`
	Downloaded     int64          `json:"downloaded"`
	Total          int64          `json:"total"`
	LastCheckedAt  string         `json:"last_checked_at,omitempty"`
	Error          string         `json:"error,omitempty"`
}

type githubUpdateRelease struct {
	TagName     string              `json:"tag_name"`
	Body        *string             `json:"body"`
	PublishedAt *string             `json:"published_at"`
	Assets      []githubUpdateAsset `json:"assets"`
}

type githubUpdateAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type updateDownloadSource struct {
	assetURL     string
	checksumURL  string
	signatureURL string
}

type updateManager struct {
	mu               sync.Mutex
	client           *http.Client
	latestReleaseURL string
	stageDirectory   string
	verifyKey        ed25519.PublicKey
	state            UpdateState
	source           updateDownloadSource
	stagedPath       string
	cancelDownload   context.CancelFunc
	emit             func(UpdateState)
}

func newUpdateManager() *updateManager {
	key, _ := parseUpdateVerifyKey(updateVerifyPublicKeyBase64)
	cacheDirectory, err := os.UserCacheDir()
	if err != nil || strings.TrimSpace(cacheDirectory) == "" {
		cacheDirectory = os.TempDir()
	}
	enabled := len(key) == ed25519.PublicKeySize && validSemanticVersion(appVersion)
	phase := updatePhaseIdle
	if !enabled {
		phase = updatePhaseDisabled
	}
	return &updateManager{
		client:           &http.Client{Timeout: 5 * time.Minute},
		latestReleaseURL: "https://api.github.com/repos/" + updateRepository + "/releases/latest",
		stageDirectory:   filepath.Join(cacheDirectory, "ATNetool", "updates"),
		verifyKey:        key,
		state: UpdateState{
			Phase:          phase,
			CurrentVersion: appVersion,
			Enabled:        enabled,
		},
	}
}

func (manager *updateManager) SetEmitter(emitter func(UpdateState)) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.emit = emitter
}

func (manager *updateManager) State() UpdateState {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return cloneUpdateState(manager.state)
}

func (manager *updateManager) Check(ctx context.Context) (UpdateState, error) {
	manager.mu.Lock()
	if !manager.state.Enabled {
		state := cloneUpdateState(manager.state)
		manager.mu.Unlock()
		return state, nil
	}
	if manager.state.Phase == updatePhaseChecking || manager.state.Phase == updatePhaseDownloading || manager.state.Phase == updatePhaseVerifying || manager.state.Phase == updatePhaseApplying {
		state := cloneUpdateState(manager.state)
		manager.mu.Unlock()
		return state, fmt.Errorf("更新任务正在进行")
	}
	manager.state.Phase = updatePhaseChecking
	manager.state.Error = ""
	manager.state.Progress = 0
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)

	release, err := manager.fetchLatestRelease(ctx)
	if err != nil {
		return manager.fail(err)
	}
	info, source, err := buildUpdateRelease(release, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return manager.fail(err)
	}

	manager.mu.Lock()
	manager.state.LastCheckedAt = time.Now().Format(time.RFC3339)
	manager.state.Release = &info
	manager.source = source
	manager.state.Progress = 0
	manager.state.Downloaded = 0
	manager.state.Total = info.AssetSize
	if newerSemanticVersion(info.Version, manager.state.CurrentVersion) {
		manager.state.Phase = updatePhaseAvailable
	} else {
		manager.state.Phase = updatePhaseUpToDate
	}
	state, emitter = manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)
	return state, nil
}

func (manager *updateManager) Download(ctx context.Context) (UpdateState, error) {
	manager.mu.Lock()
	if manager.state.Phase == updatePhaseReady {
		state := cloneUpdateState(manager.state)
		manager.mu.Unlock()
		return state, nil
	}
	if manager.state.Phase != updatePhaseAvailable || manager.state.Release == nil {
		state := cloneUpdateState(manager.state)
		manager.mu.Unlock()
		return state, fmt.Errorf("当前没有可下载的更新")
	}
	info := *manager.state.Release
	source := manager.source
	downloadContext, cancel := context.WithCancel(ctx)
	manager.cancelDownload = cancel
	manager.state.Phase = updatePhaseDownloading
	manager.state.Progress = 0
	manager.state.Downloaded = 0
	manager.state.Total = info.AssetSize
	manager.state.Error = ""
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)

	expectedHash, err := manager.fetchExpectedChecksum(downloadContext, source, info.AssetName)
	if err != nil {
		return manager.handleDownloadError(err)
	}

	if err = os.MkdirAll(manager.stageDirectory, 0o700); err != nil {
		return manager.handleDownloadError(fmt.Errorf("创建更新缓存目录失败：%w", err))
	}
	stagedPath := filepath.Join(manager.stageDirectory, info.AssetName)
	if _, statErr := os.Stat(stagedPath); statErr == nil {
		manager.setPhase(updatePhaseVerifying)
		if currentHash, hashErr := fileSHA256(stagedPath); hashErr == nil && strings.EqualFold(currentHash, expectedHash) {
			if err = downloadContext.Err(); err != nil {
				return manager.handleDownloadError(err)
			}
			return manager.markReady(downloadContext, info, stagedPath)
		}
		manager.setPhase(updatePhaseDownloading)
	}
	_ = os.Remove(stagedPath)

	if err = manager.downloadAsset(downloadContext, source.assetURL, stagedPath, expectedHash, info.AssetSize); err != nil {
		return manager.handleDownloadError(err)
	}
	if err = downloadContext.Err(); err != nil {
		return manager.handleDownloadError(err)
	}
	manager.setPhase(updatePhaseVerifying)
	return manager.markReady(downloadContext, info, stagedPath)
}

func (manager *updateManager) Cancel() UpdateState {
	manager.mu.Lock()
	if manager.cancelDownload != nil {
		manager.cancelDownload()
	}
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)
	return state
}

func (manager *updateManager) Apply() (UpdateState, error) {
	manager.mu.Lock()
	if manager.state.Phase != updatePhaseReady || manager.stagedPath == "" {
		state := cloneUpdateState(manager.state)
		manager.mu.Unlock()
		return state, fmt.Errorf("更新尚未下载完成")
	}
	stagedPath := manager.stagedPath
	manager.state.Phase = updatePhaseApplying
	manager.state.Error = ""
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)

	if err := applyStagedUpdate(stagedPath); err != nil {
		return manager.fail(err)
	}
	return state, nil
}

func (manager *updateManager) fetchLatestRelease(ctx context.Context) (githubUpdateRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manager.latestReleaseURL, nil)
	if err != nil {
		return githubUpdateRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "ATNetool/"+appVersion)
	response, err := manager.client.Do(request)
	if err != nil {
		return githubUpdateRelease{}, fmt.Errorf("检查 GitHub Release 失败：%w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		return githubUpdateRelease{}, fmt.Errorf("GitHub Release 返回 %s", response.Status)
	}
	var release githubUpdateRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, 4<<20))
	if err = decoder.Decode(&release); err != nil {
		return githubUpdateRelease{}, fmt.Errorf("解析 Release 信息失败：%w", err)
	}
	return release, nil
}

func buildUpdateRelease(release githubUpdateRelease, operatingSystem, architecture string) (UpdateRelease, updateDownloadSource, error) {
	version := strings.TrimPrefix(strings.TrimSpace(release.TagName), "v")
	if !validSemanticVersion(version) {
		return UpdateRelease{}, updateDownloadSource{}, fmt.Errorf("Release 版本号无效：%s", release.TagName)
	}
	assetName, err := expectedUpdateAssetName(version, operatingSystem, architecture)
	if err != nil {
		return UpdateRelease{}, updateDownloadSource{}, err
	}
	asset, found := findGithubUpdateAsset(release.Assets, assetName)
	if !found {
		return UpdateRelease{}, updateDownloadSource{}, fmt.Errorf("Release 中缺少 %s", assetName)
	}
	checksums, found := findGithubUpdateAsset(release.Assets, "SHA256SUMS")
	if !found {
		return UpdateRelease{}, updateDownloadSource{}, fmt.Errorf("Release 中缺少 SHA256SUMS")
	}
	signature, found := findGithubUpdateAsset(release.Assets, "SHA256SUMS.sig")
	if !found {
		return UpdateRelease{}, updateDownloadSource{}, fmt.Errorf("Release 中缺少 SHA256SUMS.sig")
	}
	notes, publishedAt := "", ""
	if release.Body != nil {
		notes = *release.Body
	}
	if release.PublishedAt != nil {
		publishedAt = *release.PublishedAt
	}
	return UpdateRelease{
			Version: version, Notes: notes, PublishedAt: publishedAt,
			AssetName: asset.Name, AssetSize: asset.Size,
		}, updateDownloadSource{
			assetURL: asset.BrowserDownloadURL, checksumURL: checksums.BrowserDownloadURL,
			signatureURL: signature.BrowserDownloadURL,
		}, nil
}

func findGithubUpdateAsset(assets []githubUpdateAsset, name string) (githubUpdateAsset, bool) {
	for _, asset := range assets {
		if asset.Name == name {
			return asset, true
		}
	}
	return githubUpdateAsset{}, false
}

func expectedUpdateAssetName(version, operatingSystem, architecture string) (string, error) {
	stem := "ATNetool_" + version
	switch operatingSystem + "/" + architecture {
	case "darwin/arm64":
		return stem + "_darwin_arm64.app.tar.gz", nil
	case "darwin/amd64":
		return stem + "_darwin_amd64.app.tar.gz", nil
	case "windows/amd64":
		return stem + "_windows_amd64.zip", nil
	case "linux/arm64":
		return stem + "_linux_arm64.tar.gz", nil
	case "linux/amd64":
		return stem + "_linux_amd64.tar.gz", nil
	default:
		return "", fmt.Errorf("暂不支持 %s/%s 自动更新", operatingSystem, architecture)
	}
}

func (manager *updateManager) fetchExpectedChecksum(ctx context.Context, source updateDownloadSource, assetName string) (string, error) {
	var failures []string
	checksumURLs := updateDownloadURLCandidates(source.checksumURL)
	signatureURLs := updateDownloadURLCandidates(source.signatureURL)
	candidateCount := min(len(checksumURLs), len(signatureURLs))
	for index := 0; index < candidateCount; index++ {
		checksumURL := checksumURLs[index]
		signatureURL := signatureURLs[index]
		checksums, err := manager.fetchAssetBytesOnce(ctx, checksumURL, 4<<20)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		signature, err := manager.fetchAssetBytesOnce(ctx, signatureURL, 32<<10)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if err = verifyChecksumsSignature(manager.verifyKey, checksums, string(signature)); err != nil {
			failures = append(failures, err.Error())
			continue
		}
		expectedHash, found := lookupExpectedChecksum(string(checksums), assetName)
		if !found {
			failures = append(failures, fmt.Sprintf("SHA256SUMS 中未找到 %s", assetName))
			continue
		}
		return expectedHash, nil
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("下载或验证更新校验文件失败：%s", strings.Join(failures, "；"))
}

func (manager *updateManager) fetchAssetBytesOnce(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "ATNetool/"+appVersion)
	response, err := manager.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("%s 返回 %s", rawURL, response.Status)
	}
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s 响应内容超过限制", rawURL)
	}
	return body, nil
}

func (manager *updateManager) downloadAsset(ctx context.Context, rawURL, destination, expectedHash string, reportedSize int64) error {
	partialPath := destination + ".part"
	defer os.Remove(partialPath)
	var failures []string
	for _, candidate := range updateDownloadURLCandidates(rawURL) {
		if err := manager.downloadAssetOnce(ctx, candidate, partialPath, expectedHash, reportedSize); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			failures = append(failures, err.Error())
			_ = os.Remove(partialPath)
			continue
		}
		if err := os.Rename(partialPath, destination); err != nil {
			return fmt.Errorf("保存更新文件失败：%w", err)
		}
		return nil
	}
	return fmt.Errorf("下载更新文件失败：%s", strings.Join(failures, "；"))
}

func (manager *updateManager) downloadAssetOnce(ctx context.Context, rawURL, partialPath, expectedHash string, reportedSize int64) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "ATNetool/"+appVersion)
	response, err := manager.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s 返回 %s", rawURL, response.Status)
	}
	total := response.ContentLength
	if total <= 0 {
		total = reportedSize
	}
	file, err := os.OpenFile(partialPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("创建更新缓存失败：%w", err)
	}
	hasher := sha256.New()
	buffer := make([]byte, 128<<10)
	var downloaded int64
	lastEmit := time.Time{}
	for {
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			if _, err = file.Write(buffer[:count]); err != nil {
				file.Close()
				return fmt.Errorf("写入更新缓存失败：%w", err)
			}
			_, _ = hasher.Write(buffer[:count])
			downloaded += int64(count)
			if lastEmit.IsZero() || time.Since(lastEmit) >= 150*time.Millisecond {
				manager.updateDownloadProgress(downloaded, total)
				lastEmit = time.Now()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			file.Close()
			return readErr
		}
		if err = ctx.Err(); err != nil {
			file.Close()
			return err
		}
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	manager.updateDownloadProgress(downloaded, total)
	actualHash := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(actualHash, expectedHash) {
		return fmt.Errorf("更新文件 SHA-256 校验失败")
	}
	return nil
}

func (manager *updateManager) updateDownloadProgress(downloaded, total int64) {
	manager.mu.Lock()
	if manager.state.Phase != updatePhaseDownloading {
		manager.mu.Unlock()
		return
	}
	manager.state.Downloaded = downloaded
	manager.state.Total = total
	if total > 0 {
		manager.state.Progress = int(min(int64(100), downloaded*100/total))
	}
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)
}

func (manager *updateManager) setPhase(phase string) {
	manager.mu.Lock()
	manager.state.Phase = phase
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)
}

func (manager *updateManager) markReady(ctx context.Context, info UpdateRelease, stagedPath string) (UpdateState, error) {
	manager.mu.Lock()
	if err := ctx.Err(); err != nil {
		manager.cancelDownload = nil
		manager.state.Progress = 0
		manager.state.Downloaded = 0
		if errors.Is(err, context.Canceled) {
			manager.state.Phase = updatePhaseAvailable
			manager.state.Error = ""
		} else {
			manager.state.Phase = updatePhaseError
			manager.state.Error = err.Error()
		}
		state, emitter := manager.snapshotAndEmitterLocked()
		manager.mu.Unlock()
		emitUpdateState(emitter, state)
		if errors.Is(err, context.Canceled) {
			return state, nil
		}
		return state, err
	}
	manager.cancelDownload = nil
	manager.stagedPath = stagedPath
	manager.state.Phase = updatePhaseReady
	manager.state.Progress = 100
	manager.state.Downloaded = info.AssetSize
	manager.state.Total = info.AssetSize
	manager.state.Error = ""
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)
	return state, nil
}

func (manager *updateManager) handleDownloadError(err error) (UpdateState, error) {
	manager.mu.Lock()
	manager.cancelDownload = nil
	if errors.Is(err, context.Canceled) {
		manager.state.Phase = updatePhaseAvailable
		manager.state.Progress = 0
		manager.state.Downloaded = 0
		manager.state.Error = ""
	} else {
		manager.state.Phase = updatePhaseError
		manager.state.Error = err.Error()
	}
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)
	if errors.Is(err, context.Canceled) {
		return state, nil
	}
	return state, err
}

func (manager *updateManager) fail(err error) (UpdateState, error) {
	manager.mu.Lock()
	manager.state.Phase = updatePhaseError
	manager.state.Error = err.Error()
	state, emitter := manager.snapshotAndEmitterLocked()
	manager.mu.Unlock()
	emitUpdateState(emitter, state)
	return state, err
}

func (manager *updateManager) snapshotAndEmitterLocked() (UpdateState, func(UpdateState)) {
	return cloneUpdateState(manager.state), manager.emit
}

func cloneUpdateState(state UpdateState) UpdateState {
	cloned := state
	if state.Release != nil {
		release := *state.Release
		cloned.Release = &release
	}
	return cloned
}

func emitUpdateState(emitter func(UpdateState), state UpdateState) {
	if emitter != nil {
		emitter(state)
	}
}

func parseUpdateVerifyKey(encoded string) (ed25519.PublicKey, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, fmt.Errorf("更新验证公钥未配置")
	}
	pemBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("更新验证公钥 Base64 无效：%w", err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("更新验证公钥 PEM 无效")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析更新验证公钥失败：%w", err)
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("更新验证公钥不是 Ed25519")
	}
	return key, nil
}

func verifyChecksumsSignature(key ed25519.PublicKey, checksums []byte, signatureBase64 string) error {
	if len(key) != ed25519.PublicKeySize {
		return fmt.Errorf("更新验证公钥未配置")
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(signatureBase64))
	if err != nil {
		return fmt.Errorf("SHA256SUMS.sig Base64 无效：%w", err)
	}
	if len(signature) != ed25519.SignatureSize || !ed25519.Verify(key, checksums, signature) {
		return fmt.Errorf("SHA256SUMS 签名验证失败")
	}
	return nil
}

func lookupExpectedChecksum(checksums, filename string) (string, bool) {
	for _, line := range strings.Split(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[1] != filename {
			continue
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", false
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", false
		}
		return strings.ToLower(fields[0]), true
	}
	return "", false
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err = io.Copy(hasher, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func updateDownloadURLCandidates(rawURL string) []string {
	if !strings.HasPrefix(rawURL, "https://github.com/") && !strings.HasPrefix(rawURL, "https://www.github.com/") {
		return []string{rawURL}
	}
	mirrors := defaultUpdateMirrors
	if configured := strings.TrimSpace(os.Getenv("ATNETOOL_UPDATE_DOWNLOAD_MIRRORS")); configured != "" {
		mirrors = strings.FieldsFunc(configured, func(character rune) bool {
			return character == ',' || character == ';' || character == '\n'
		})
	}
	candidates := make([]string, 0, len(mirrors)+1)
	seen := make(map[string]struct{})
	for _, mirror := range mirrors {
		mirror = strings.TrimSpace(mirror)
		if mirror == "" {
			continue
		}
		candidate := strings.TrimRight(mirror, "/") + "/" + rawURL
		if strings.Contains(mirror, "{url}") {
			candidate = strings.ReplaceAll(mirror, "{url}", rawURL)
		}
		if _, exists := seen[candidate]; !exists {
			seen[candidate] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	if _, exists := seen[rawURL]; !exists {
		candidates = append(candidates, rawURL)
	}
	return candidates
}

type semanticVersion struct {
	major      uint64
	minor      uint64
	patch      uint64
	prerelease []string
}

func validSemanticVersion(raw string) bool {
	_, ok := parseSemanticVersion(raw)
	return ok
}

func newerSemanticVersion(candidate, current string) bool {
	candidateVersion, candidateOK := parseSemanticVersion(candidate)
	currentVersion, currentOK := parseSemanticVersion(current)
	if !candidateOK || !currentOK {
		return false
	}
	return compareSemanticVersions(candidateVersion, currentVersion) > 0
}

func parseSemanticVersion(raw string) (semanticVersion, bool) {
	raw = strings.TrimPrefix(strings.TrimSpace(raw), "v")
	coreAndPrerelease := strings.SplitN(strings.SplitN(raw, "+", 2)[0], "-", 2)
	parts := strings.Split(coreAndPrerelease[0], ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}
	numbers := make([]uint64, 3)
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semanticVersion{}, false
		}
		value, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return semanticVersion{}, false
		}
		numbers[index] = value
	}
	version := semanticVersion{major: numbers[0], minor: numbers[1], patch: numbers[2]}
	if len(coreAndPrerelease) == 2 {
		if coreAndPrerelease[1] == "" {
			return semanticVersion{}, false
		}
		version.prerelease = strings.Split(coreAndPrerelease[1], ".")
		for _, identifier := range version.prerelease {
			if identifier == "" {
				return semanticVersion{}, false
			}
			for _, character := range identifier {
				if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || character == '-') {
					return semanticVersion{}, false
				}
			}
		}
	}
	return version, true
}

func compareSemanticVersions(left, right semanticVersion) int {
	for _, pair := range [][2]uint64{{left.major, right.major}, {left.minor, right.minor}, {left.patch, right.patch}} {
		if pair[0] < pair[1] {
			return -1
		}
		if pair[0] > pair[1] {
			return 1
		}
	}
	if len(left.prerelease) == 0 && len(right.prerelease) > 0 {
		return 1
	}
	if len(left.prerelease) > 0 && len(right.prerelease) == 0 {
		return -1
	}
	for index := 0; index < min(len(left.prerelease), len(right.prerelease)); index++ {
		leftIdentifier, rightIdentifier := left.prerelease[index], right.prerelease[index]
		if leftIdentifier == rightIdentifier {
			continue
		}
		leftNumber, leftErr := strconv.ParseUint(leftIdentifier, 10, 64)
		rightNumber, rightErr := strconv.ParseUint(rightIdentifier, 10, 64)
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber < rightNumber {
				return -1
			}
			return 1
		case leftErr == nil:
			return -1
		case rightErr == nil:
			return 1
		case leftIdentifier < rightIdentifier:
			return -1
		default:
			return 1
		}
	}
	if len(left.prerelease) < len(right.prerelease) {
		return -1
	}
	if len(left.prerelease) > len(right.prerelease) {
		return 1
	}
	return 0
}
