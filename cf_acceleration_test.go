package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type fakeSystemProxyController struct {
	snapshot    SystemProxySnapshot
	appliedHost string
	appliedPort int
	applyErr    error
	restoreErr  error
	restored    bool
	matches     bool
}

func (controller *fakeSystemProxyController) Capture(context.Context) (SystemProxySnapshot, error) {
	return controller.snapshot, nil
}

func (controller *fakeSystemProxyController) Apply(_ context.Context, host string, port int) error {
	controller.appliedHost, controller.appliedPort = host, port
	if controller.applyErr == nil {
		controller.matches = true
	}
	return controller.applyErr
}

func (controller *fakeSystemProxyController) Restore(context.Context, SystemProxySnapshot) error {
	controller.restored = true
	if controller.restoreErr == nil {
		controller.matches = false
	}
	return controller.restoreErr
}

func (controller *fakeSystemProxyController) Matches(context.Context, string, int) bool {
	return controller.matches
}

func TestCFAccelerationManagerAppliesAndRestoresTransaction(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("CF-Ray", "acceleration-test-HKG")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	controller := &fakeSystemProxyController{snapshot: SystemProxySnapshot{Platform: runtime.GOOS}}
	manager := &cfAccelerationManager{
		controller:   controller,
		recoveryPath: filepath.Join(t.TempDir(), "proxy-recovery.json"),
		status:       CFAccelerationStatus{State: "inactive"},
	}
	result := validCFAccelerationResult("edge.example", "127.0.0.1", "http://edge.example:"+port+"/")
	status, err := manager.Apply(context.Background(), result)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Active || !status.SystemProxyApplied || controller.appliedHost != "127.0.0.1" || controller.appliedPort == 0 {
		t.Fatalf("status=%+v controller=%+v", status, controller)
	}
	if _, err = os.Stat(manager.recoveryPath); err != nil {
		t.Fatalf("recovery point missing: %v", err)
	}
	status, err = manager.Stop(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Active || status.State != "inactive" || !controller.restored {
		t.Fatalf("status=%+v controller=%+v", status, controller)
	}
	if _, err = os.Stat(manager.recoveryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recovery point was not removed: %v", err)
	}
}

func TestCFAccelerationManagerRollsBackFailedSystemProxyApply(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	controller := &fakeSystemProxyController{
		snapshot: SystemProxySnapshot{Platform: runtime.GOOS},
		applyErr: errors.New("apply failed"),
	}
	manager := &cfAccelerationManager{
		controller:   controller,
		recoveryPath: filepath.Join(t.TempDir(), "proxy-recovery.json"),
		status:       CFAccelerationStatus{State: "inactive"},
	}
	_, err := manager.Apply(context.Background(), validCFAccelerationResult("edge.example", "127.0.0.1", "http://edge.example:"+port+"/"))
	if err == nil || !controller.restored || manager.gateway != nil {
		t.Fatalf("err=%v controller=%+v gateway=%v", err, controller, manager.gateway)
	}
	if _, statErr := os.Stat(manager.recoveryPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("recovery point remains after rollback: %v", statErr)
	}
}

func TestCFAccelerationManagerKeepsRecoveryWhenRollbackFails(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer origin.Close()
	_, port, _ := net.SplitHostPort(origin.Listener.Addr().String())
	controller := &fakeSystemProxyController{
		snapshot:   SystemProxySnapshot{Platform: runtime.GOOS},
		applyErr:   errors.New("apply failed"),
		restoreErr: errors.New("restore failed"),
	}
	manager := &cfAccelerationManager{
		controller:   controller,
		recoveryPath: filepath.Join(t.TempDir(), "proxy-recovery.json"),
		status:       CFAccelerationStatus{State: "inactive"},
	}
	status, err := manager.Apply(context.Background(), validCFAccelerationResult("edge.example", "127.0.0.1", "http://edge.example:"+port+"/"))
	if err == nil {
		t.Fatal("expected apply and rollback failure")
	}
	if !status.CanRestore || manager.gateway == nil || manager.snapshot == nil {
		t.Fatalf("recovery state was not retained: status=%+v gateway=%v snapshot=%v", status, manager.gateway, manager.snapshot)
	}
	defer manager.gateway.Close(context.Background())
	if _, statErr := os.Stat(manager.recoveryPath); statErr != nil {
		t.Fatalf("recovery point must remain after rollback failure: %v", statErr)
	}
}

func TestCFAccelerationManagerRecoversStaleSystemProxy(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "proxy-recovery.json")
	snapshot := SystemProxySnapshot{Platform: runtime.GOOS, Values: map[string]string{"mode": "original"}}
	if err := writeCFAccelerationRecovery(path, cfAccelerationRecovery{
		Version: cfAccelerationRecoveryVersion, GatewayHost: "127.0.0.1", GatewayPort: 48123, Snapshot: snapshot,
	}); err != nil {
		t.Fatal(err)
	}
	controller := &fakeSystemProxyController{snapshot: snapshot, matches: true}
	manager := &cfAccelerationManager{controller: controller, recoveryPath: path, status: CFAccelerationStatus{State: "inactive"}}
	status := manager.RecoverStale(context.Background())
	if !status.Recovered || !controller.restored {
		t.Fatalf("status=%+v controller=%+v", status, controller)
	}
}

func validCFAccelerationResult(domain, bestIP, targetURL string) CDNOptimizeResult {
	return CDNOptimizeResult{
		Domain:             domain,
		BestIP:             bestIP,
		TargetURL:          targetURL,
		EffectiveRoute:     cdnRouteDirect,
		TargetValidated:    true,
		TargetVerification: CDNTargetVerification{Verified: true},
	}
}
