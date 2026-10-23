package main

import (
	"context"
	"net"
	"reflect"
	"testing"
)

func TestNormalisePortTarget(t *testing.T) {
	if got := normalisePortTarget("https://Example.COM:8443/path"); got != "example.com" {
		t.Fatalf("target = %q", got)
	}
	if got := normalisePortTarget("[2001:db8::1]:443"); got != "2001:db8::1" {
		t.Fatalf("IPv6 target = %q", got)
	}
}

func TestNormalisePorts(t *testing.T) {
	got := normalisePorts([]int{443, 80, 443, 0, 70000, 22})
	want := []int{22, 80, 443}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ports = %+v, want %+v", got, want)
	}
}

func TestProbePortDetectsOpenTCPPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = connection.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	probe := probePort(context.Background(), 0, "127.0.0.1", []string{"127.0.0.1"}, port)
	if !probe.Reachable || probe.Status != "open" || probe.Port != port {
		t.Fatalf("probe = %+v", probe)
	}
}

func TestBuildPortTestFindingsDistinguishesRefusedAndTimeout(t *testing.T) {
	result := PortTestResult{TotalCount: 3, OpenCount: 1, RefusedCount: 1, TimeoutCount: 1, ResolvedIPs: []string{"203.0.113.1"}}
	findings, remediations := buildPortTestFindings(result)
	if len(findings) != 3 || len(remediations) != 2 {
		t.Fatalf("findings = %+v, remediations = %+v", findings, remediations)
	}
}
