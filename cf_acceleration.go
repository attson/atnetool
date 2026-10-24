package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const cfAccelerationRecoveryVersion = 1

type CFAccelerationStatus struct {
	Active             bool   `json:"active"`
	State              string `json:"state"`
	Domain             string `json:"domain,omitempty"`
	PreferredIP        string `json:"preferred_ip,omitempty"`
	GatewayURL         string `json:"gateway_url,omitempty"`
	ManagedUpstreamURL string `json:"managed_upstream_url,omitempty"`
	FallbackProxyURL   string `json:"fallback_proxy_url,omitempty"`
	EffectiveRoute     string `json:"effective_route,omitempty"`
	StartedAt          string `json:"started_at,omitempty"`
	SystemProxyApplied bool   `json:"system_proxy_applied"`
	CanRestore         bool   `json:"can_restore"`
	RequestCount       uint64 `json:"request_count"`
	Recovered          bool   `json:"recovered"`
	Message            string `json:"message"`
}

type cfAccelerationRecovery struct {
	Version     int                 `json:"version"`
	CreatedAt   string              `json:"created_at"`
	GatewayHost string              `json:"gateway_host"`
	GatewayPort int                 `json:"gateway_port"`
	Snapshot    SystemProxySnapshot `json:"snapshot"`
}

type systemProxyController interface {
	Capture(context.Context) (SystemProxySnapshot, error)
	Apply(context.Context, string, int) error
	Restore(context.Context, SystemProxySnapshot) error
	Matches(context.Context, string, int) bool
}

type nativeSystemProxyController struct{}

func (nativeSystemProxyController) Capture(ctx context.Context) (SystemProxySnapshot, error) {
	return capturePlatformSystemProxy(ctx)
}

func (nativeSystemProxyController) Apply(ctx context.Context, host string, port int) error {
	return applyPlatformSystemProxy(ctx, host, port)
}

func (nativeSystemProxyController) Restore(ctx context.Context, snapshot SystemProxySnapshot) error {
	return restorePlatformSystemProxy(ctx, snapshot)
}

func (nativeSystemProxyController) Matches(ctx context.Context, host string, port int) bool {
	return platformSystemProxyMatches(ctx, host, port)
}

type cfAccelerationManager struct {
	mu           sync.Mutex
	controller   systemProxyController
	recoveryPath string
	gateway      *cfGateway
	snapshot     *SystemProxySnapshot
	status       CFAccelerationStatus
}

func newCFAccelerationManager() *cfAccelerationManager {
	return &cfAccelerationManager{
		controller:   nativeSystemProxyController{},
		recoveryPath: defaultCFAccelerationRecoveryPath(),
		status: CFAccelerationStatus{
			State:   "inactive",
			Message: "未启用 CF IP 加速",
		},
	}
}

func defaultCFAccelerationRecoveryPath() string {
	base, err := os.UserConfigDir()
	if err != nil || strings.TrimSpace(base) == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "ATNetool", "proxy-recovery.json")
}

func (a *App) GetCFAccelerationStatus() CFAccelerationStatus {
	if a.acceleration == nil {
		return CFAccelerationStatus{State: "inactive", Message: "未启用 CF IP 加速"}
	}
	return a.acceleration.Status()
}

func (a *App) ApplyCFAcceleration(result CDNOptimizeResult) (CFAccelerationStatus, error) {
	if a.acceleration == nil {
		return CFAccelerationStatus{}, fmt.Errorf("兼容网关尚未初始化")
	}
	ctx, cancel := context.WithTimeout(a.baseContext(), 25*time.Second)
	defer cancel()
	return a.acceleration.Apply(ctx, result)
}

func (a *App) StopCFAcceleration() (CFAccelerationStatus, error) {
	if a.acceleration == nil {
		return CFAccelerationStatus{State: "inactive", Message: "未启用 CF IP 加速"}, nil
	}
	ctx, cancel := context.WithTimeout(a.baseContext(), 12*time.Second)
	defer cancel()
	return a.acceleration.Stop(ctx)
}

func (manager *cfAccelerationManager) Status() CFAccelerationStatus {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.statusLocked()
}

func (manager *cfAccelerationManager) statusLocked() CFAccelerationStatus {
	status := manager.status
	if manager.gateway != nil {
		status.RequestCount = manager.gateway.RequestCount()
		if manager.gateway.closed.Load() && (status.Active || status.CanRestore) {
			status.Active = false
			status.State = "error"
			status.CanRestore = true
			status.Message = "本地兼容网关意外停止，请立即恢复系统代理"
		}
	}
	return status
}

func (manager *cfAccelerationManager) Apply(ctx context.Context, result CDNOptimizeResult) (CFAccelerationStatus, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.status.CanRestore && !manager.status.Active {
		return manager.statusLocked(), fmt.Errorf("存在尚未恢复的系统代理快照，请先重试恢复")
	}
	if err := validateCFAccelerationResult(result); err != nil {
		return manager.statusLocked(), err
	}
	if manager.status.Active {
		if err := manager.stopLocked(ctx, "正在切换优选节点"); err != nil {
			return manager.statusLocked(), err
		}
	}
	manager.status = CFAccelerationStatus{
		State:          "starting",
		Domain:         result.Domain,
		PreferredIP:    result.BestIP,
		EffectiveRoute: result.EffectiveRoute,
		Message:        "正在启动本地兼容网关",
	}

	snapshot, err := manager.controller.Capture(ctx)
	if err != nil {
		manager.status.State = "error"
		manager.status.Message = err.Error()
		return manager.statusLocked(), err
	}
	managedUpstream, err := managedCFAccelerationUpstream(result)
	if err != nil {
		manager.status.State = "error"
		manager.status.Message = err.Error()
		return manager.statusLocked(), err
	}
	defaultHTTPUpstream, err := currentProxyForTargetScheme(result.Domain, "http")
	if err != nil {
		manager.status.State = "error"
		manager.status.Message = "读取当前代理出口失败"
		return manager.statusLocked(), err
	}
	defaultHTTPSUpstream, err := currentProxyForTargetScheme(result.Domain, "https")
	if err != nil {
		manager.status.State = "error"
		manager.status.Message = "读取当前代理出口失败"
		return manager.statusLocked(), err
	}
	gateway, err := startCFGateway(cfGatewayConfig{
		Domain:               result.Domain,
		PreferredIP:          result.BestIP,
		ManagedUpstream:      managedUpstream,
		DefaultHTTPUpstream:  defaultHTTPUpstream,
		DefaultHTTPSUpstream: defaultHTTPSUpstream,
	})
	if err != nil {
		manager.status.State = "error"
		manager.status.Message = err.Error()
		return manager.statusLocked(), err
	}
	closeGateway := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = gateway.Close(closeCtx)
	}
	if err = preflightCFGateway(ctx, gateway, result.TargetURL); err != nil {
		closeGateway()
		manager.status.State = "error"
		manager.status.Message = "兼容网关预检失败，系统代理未修改"
		return manager.statusLocked(), fmt.Errorf("兼容网关预检失败：%w", err)
	}
	host, port, err := splitGatewayEndpoint(gateway.Endpoint())
	if err != nil {
		closeGateway()
		return manager.statusLocked(), err
	}
	recovery := cfAccelerationRecovery{
		Version: cfAccelerationRecoveryVersion, CreatedAt: time.Now().Format(time.RFC3339),
		GatewayHost: host, GatewayPort: port, Snapshot: snapshot,
	}
	if err = writeCFAccelerationRecovery(manager.recoveryPath, recovery); err != nil {
		closeGateway()
		manager.status.State = "error"
		manager.status.Message = "无法保存系统代理恢复点"
		return manager.statusLocked(), err
	}
	if err = manager.controller.Apply(ctx, host, port); err != nil {
		restoreErr := manager.controller.Restore(ctx, snapshot)
		if restoreErr != nil {
			manager.retainGatewayForRecoveryLocked(ctx, gateway, snapshot, result, managedUpstream, defaultHTTPSUpstream, host, port)
			return manager.statusLocked(), errors.Join(err, restoreErr)
		}
		_ = removeCFAccelerationRecovery(manager.recoveryPath)
		closeGateway()
		manager.status.State = "error"
		manager.status.Message = "系统代理切换失败，已恢复原设置"
		return manager.statusLocked(), errors.Join(err, restoreErr)
	}
	if !manager.controller.Matches(ctx, host, port) {
		restoreErr := manager.controller.Restore(ctx, snapshot)
		if restoreErr != nil {
			manager.retainGatewayForRecoveryLocked(ctx, gateway, snapshot, result, managedUpstream, defaultHTTPSUpstream, host, port)
			return manager.statusLocked(), errors.Join(fmt.Errorf("系统代理未切换到本地网关"), restoreErr)
		}
		_ = removeCFAccelerationRecovery(manager.recoveryPath)
		closeGateway()
		manager.status.State = "error"
		manager.status.Message = "系统代理校验失败，已恢复原设置"
		return manager.statusLocked(), errors.Join(fmt.Errorf("系统代理未切换到本地网关"), restoreErr)
	}
	manager.gateway = gateway
	manager.snapshot = &snapshot
	manager.status = CFAccelerationStatus{
		Active:             true,
		State:              "active",
		Domain:             result.Domain,
		PreferredIP:        result.BestIP,
		GatewayURL:         gateway.URL(),
		ManagedUpstreamURL: safeProxyURL(managedUpstream),
		FallbackProxyURL:   safeProxyURL(defaultHTTPSUpstream),
		EffectiveRoute:     result.EffectiveRoute,
		StartedAt:          time.Now().Format(time.RFC3339),
		SystemProxyApplied: true,
		CanRestore:         true,
		Message:            "CF IP 优选已应用，其他流量保持原出口",
	}
	return manager.statusLocked(), nil
}

func (manager *cfAccelerationManager) retainGatewayForRecoveryLocked(
	ctx context.Context,
	gateway *cfGateway,
	snapshot SystemProxySnapshot,
	result CDNOptimizeResult,
	managedUpstream, fallbackUpstream *url.URL,
	host string,
	port int,
) {
	manager.gateway = gateway
	manager.snapshot = &snapshot
	manager.status = CFAccelerationStatus{
		State:              "error",
		Domain:             result.Domain,
		PreferredIP:        result.BestIP,
		GatewayURL:         gateway.URL(),
		ManagedUpstreamURL: safeProxyURL(managedUpstream),
		FallbackProxyURL:   safeProxyURL(fallbackUpstream),
		EffectiveRoute:     result.EffectiveRoute,
		SystemProxyApplied: manager.controller.Matches(ctx, host, port),
		CanRestore:         true,
		Message:            "自动恢复原系统代理失败，本地网关保持运行，请重试恢复",
	}
}

func (manager *cfAccelerationManager) Stop(ctx context.Context) (CFAccelerationStatus, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if err := manager.stopLocked(ctx, "正在恢复原系统代理"); err != nil {
		return manager.statusLocked(), err
	}
	return manager.statusLocked(), nil
}

func (manager *cfAccelerationManager) stopLocked(ctx context.Context, message string) error {
	if !manager.status.Active && manager.gateway == nil {
		if recovery, err := readCFAccelerationRecovery(manager.recoveryPath); err == nil {
			if !manager.controller.Matches(ctx, recovery.GatewayHost, recovery.GatewayPort) {
				manager.status = CFAccelerationStatus{State: "warning", CanRestore: true, Message: "当前系统代理与恢复点不一致，未覆盖用户设置"}
				return fmt.Errorf("当前系统代理与恢复点不一致")
			}
			if err = manager.controller.Restore(ctx, recovery.Snapshot); err != nil {
				manager.status = CFAccelerationStatus{State: "error", CanRestore: true, Message: "恢复原系统代理失败"}
				return err
			}
			_ = removeCFAccelerationRecovery(manager.recoveryPath)
			manager.status = CFAccelerationStatus{State: "inactive", Recovered: true, Message: "已恢复原系统代理"}
			return nil
		} else if !errors.Is(err, os.ErrNotExist) {
			manager.status = CFAccelerationStatus{State: "error", CanRestore: true, Message: "系统代理恢复点无法读取"}
			return err
		}
		manager.status.State = "inactive"
		manager.status.Message = "未启用 CF IP 加速"
		manager.status.CanRestore = false
		return nil
	}
	manager.status.State = "stopping"
	manager.status.Message = message
	if manager.snapshot == nil {
		manager.status.State = "error"
		manager.status.Message = "缺少系统代理恢复点，本地网关保持运行"
		return fmt.Errorf("缺少系统代理恢复点")
	}
	if err := manager.controller.Restore(ctx, *manager.snapshot); err != nil {
		manager.status.State = "error"
		manager.status.Message = "恢复原系统代理失败，本地网关保持运行"
		return err
	}
	_ = removeCFAccelerationRecovery(manager.recoveryPath)
	gateway := manager.gateway
	manager.gateway = nil
	manager.snapshot = nil
	if gateway != nil {
		if err := gateway.Close(ctx); err != nil && !errors.Is(err, context.Canceled) {
			manager.status = CFAccelerationStatus{State: "inactive", Message: "原系统代理已恢复，本地网关关闭超时"}
			return err
		}
	}
	manager.status = CFAccelerationStatus{State: "inactive", Message: "已恢复原系统代理"}
	return nil
}

func (manager *cfAccelerationManager) RecoverStale(ctx context.Context) CFAccelerationStatus {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	recovery, err := readCFAccelerationRecovery(manager.recoveryPath)
	if errors.Is(err, os.ErrNotExist) {
		return manager.statusLocked()
	}
	if err != nil {
		manager.status = CFAccelerationStatus{State: "warning", CanRestore: true, Message: "发现损坏的系统代理恢复点，请检查系统代理设置"}
		return manager.statusLocked()
	}
	if recovery.Version != cfAccelerationRecoveryVersion || recovery.Snapshot.Platform != runtime.GOOS {
		manager.status = CFAccelerationStatus{State: "warning", CanRestore: true, Message: "发现不兼容的系统代理恢复点，请手动检查代理设置"}
		return manager.statusLocked()
	}
	if !manager.controller.Matches(ctx, recovery.GatewayHost, recovery.GatewayPort) {
		_ = removeCFAccelerationRecovery(manager.recoveryPath)
		manager.status = CFAccelerationStatus{State: "inactive", Message: "检测到系统代理已由用户修改，未覆盖当前设置"}
		return manager.statusLocked()
	}
	if err = manager.controller.Restore(ctx, recovery.Snapshot); err != nil {
		manager.status = CFAccelerationStatus{State: "error", CanRestore: true, Message: "上次异常退出后的系统代理恢复失败"}
		return manager.statusLocked()
	}
	_ = removeCFAccelerationRecovery(manager.recoveryPath)
	manager.status = CFAccelerationStatus{State: "inactive", Recovered: true, Message: "已恢复上次异常退出前的系统代理"}
	return manager.statusLocked()
}

func validateCFAccelerationResult(result CDNOptimizeResult) error {
	if strings.TrimSpace(result.Domain) == "" || net.ParseIP(result.Domain) != nil {
		return fmt.Errorf("缺少有效的 Cloudflare 目标域名")
	}
	if net.ParseIP(strings.TrimSpace(result.BestIP)) == nil {
		return fmt.Errorf("缺少有效的推荐 IP")
	}
	if !result.TargetVerification.Verified || !result.TargetValidated {
		return fmt.Errorf("推荐 IP 尚未通过目标域名与 Cloudflare 验证")
	}
	if result.EffectiveRoute != cdnRouteDirect && result.EffectiveRoute != cdnRouteTunnel && result.EffectiveRoute != cdnRouteProxy {
		return fmt.Errorf("缺少可信的候选测试出口")
	}
	if _, err := validateHTTPURL(result.TargetURL); err != nil {
		return fmt.Errorf("目标地址无效：%w", err)
	}
	return nil
}

func managedCFAccelerationUpstream(result CDNOptimizeResult) (*url.URL, error) {
	if result.EffectiveRoute != cdnRouteProxy {
		return nil, nil
	}
	raw := preserveSystemProxyCredentials(result.UpstreamProxyURL, result.TargetURL, result.ProxyCompatibility)
	proxyURL, err := validateProxyURL(raw)
	if err != nil {
		return nil, fmt.Errorf("无法恢复候选测试使用的代理出口：%w", err)
	}
	return proxyURL, nil
}

func currentProxyForTargetScheme(domain, scheme string) (*url.URL, error) {
	request, err := http.NewRequest(http.MethodGet, scheme+"://"+domain+"/", nil)
	if err != nil {
		return nil, err
	}
	return systemProxyForRequest(request)
}

func preflightCFGateway(ctx context.Context, gateway *cfGateway, targetURL string) error {
	proxyURL, err := url.Parse(gateway.URL())
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "ATNetool/1.0")
	request.Header.Set("Range", "bytes=0-0")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	transport.DisableKeepAlives = true
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.CopyN(io.Discard, response.Body, 4096)
	if response.Header.Get(cfGatewayErrorHeader) != "" {
		return fmt.Errorf("本地网关返回 %s", response.Status)
	}
	return nil
}

func splitGatewayEndpoint(endpoint string) (string, int, error) {
	host, rawPort, err := net.SplitHostPort(endpoint)
	if err != nil {
		return "", 0, err
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("本地网关端口无效")
	}
	return host, port, nil
}

func writeCFAccelerationRecovery(path string, recovery cfAccelerationRecovery) error {
	data, err := json.MarshalIndent(recovery, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".proxy-recovery-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func readCFAccelerationRecovery(path string) (cfAccelerationRecovery, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cfAccelerationRecovery{}, err
	}
	var recovery cfAccelerationRecovery
	if err = json.Unmarshal(data, &recovery); err != nil {
		return cfAccelerationRecovery{}, err
	}
	return recovery, nil
}

func removeCFAccelerationRecovery(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
