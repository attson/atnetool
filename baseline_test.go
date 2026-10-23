package main

import (
	"bufio"
	"net"
	"testing"
	"time"
)

func TestRunNetworkBaselineRejectsInvalidTarget(t *testing.T) {
	result := NewApp().RunNetworkBaseline(NetworkBaselineOptions{TargetHost: "https://bad host/path"})
	if result.Status != "failed" || result.Message == "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCompareDNSAnswersDistinguishesOverlapFromDifference(t *testing.T) {
	probes := []DNSProbeResult{
		{Name: "系统 DNS", Success: true, IPv4: []string{"203.0.113.10", "203.0.113.11"}},
		{Name: "Cloudflare", Success: true, IPv4: []string{"203.0.113.11"}},
		{Name: "Google", Success: true, IPv4: []string{"198.51.100.20"}},
	}

	comparison := compareDNSAnswers(probes)
	if comparison.SuccessfulResolvers != 3 || comparison.MatchingResolvers != 2 || !comparison.Different {
		t.Fatalf("comparison = %+v", comparison)
	}
}

func TestBaselineFindingsTreatProxyOnlyAccessAsRestrictedNotOffline(t *testing.T) {
	result := NetworkBaselineResult{
		Environment:    NetworkEnvironment{Interfaces: []NetworkInterfaceSnapshot{{Name: "eth0"}}},
		DNSProbes:      []DNSProbeResult{{Name: "系统 DNS", Success: true}},
		DNSComparison:  DNSComparison{SuccessfulResolvers: 1, MatchingResolvers: 1},
		IPStacks:       []IPStackProbeResult{{Family: "IPv4"}, {Family: "IPv6"}},
		ProxyListeners: []ProxyListenerResult{{Endpoint: "127.0.0.1:7890", Listening: true, Protocol: "SOCKS5"}},
	}

	findings := buildBaselineFindings(result)
	if len(findings) != 4 || findings[2].Status != "warning" || findings[2].Title != "公网访问依赖代理" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestProbeProxyEndpointRecognisesSOCKS5(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(time.Second))
				reader := bufio.NewReader(conn)
				first, readErr := reader.ReadByte()
				if readErr == nil && first == 0x05 {
					_, _ = reader.ReadByte()
					_, _ = reader.ReadByte()
					_, _ = conn.Write([]byte{0x05, 0x00})
				}
			}(connection)
		}
	}()

	result := probeProxyEndpoint(listener.Addr().String(), "common")
	if !result.Listening || result.Protocol != "SOCKS5" {
		t.Fatalf("result = %+v", result)
	}
}

func TestClassifyNetworkInterfaceRecognisesTunnelAndVirtualAdapters(t *testing.T) {
	tests := []struct {
		name        string
		hardware    string
		wantType    string
		wantVirtual bool
		wantTunnel  bool
	}{
		{name: "utun4", wantType: "tunnel", wantVirtual: true, wantTunnel: true},
		{name: "wg0", wantType: "tunnel", wantVirtual: true, wantTunnel: true},
		{name: "WireGuard Tunnel", hardware: "00:ff:00:ff:00:ff", wantType: "tunnel", wantVirtual: true, wantTunnel: true},
		{name: "docker0", hardware: "02:42:aa:bb:cc:dd", wantType: "virtual", wantVirtual: true},
		{name: "eth0", hardware: "00:11:22:33:44:55", wantType: "physical"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gotType, gotVirtual, gotTunnel := classifyNetworkInterface(test.name, test.hardware)
			if gotType != test.wantType || gotVirtual != test.wantVirtual || gotTunnel != test.wantTunnel {
				t.Fatalf("classifyNetworkInterface(%q) = %q, %v, %v", test.name, gotType, gotVirtual, gotTunnel)
			}
		})
	}
}

func TestBuildLocalNetworkAssessmentDetectsTunnelRouteTakeover(t *testing.T) {
	result := NetworkBaselineResult{
		TargetHost: "example.com",
		Routes: []DefaultRoute{
			{Family: "IPv4", Interface: "utun4", Metric: 5},
			{Family: "IPv4", Interface: "en0", Gateway: "192.168.1.1", Metric: 100},
		},
		IPStacks: []IPStackProbeResult{{Family: "IPv4", Available: true, Interface: "utun4"}},
		Environment: NetworkEnvironment{Interfaces: []NetworkInterfaceSnapshot{
			{Name: "en0", MTU: 1500, Type: "physical"},
			{Name: "utun4", MTU: 1200, Type: "tunnel", Virtual: true, Tunnel: true, MTURisk: true},
		}},
	}

	assessment := buildLocalNetworkAssessment(&result)
	if !assessment.RouteTakeover || !assessment.MultipleDefaultRoutes || assessment.Status != "warning" {
		t.Fatalf("assessment = %+v", assessment)
	}
	if len(assessment.MTURisks) != 1 || len(assessment.ActiveInterfaces) != 2 || !result.Environment.Interfaces[1].DefaultRoute {
		t.Fatalf("assessment/environment = %+v / %+v", assessment, result.Environment)
	}
}

func TestBuildBaselineRemediationsIncludesRouteAndMTUActions(t *testing.T) {
	result := NetworkBaselineResult{
		DNSProbes:     []DNSProbeResult{{Name: "system"}, {Name: "public"}},
		DNSComparison: DNSComparison{SuccessfulResolvers: 1, Different: true},
		LocalNetwork: LocalNetworkAssessment{
			GatewayBindingComplete: false,
			MultipleDefaultRoutes:  true,
			RouteTakeover:          true,
			MTURisks:               []string{"utun4 (1200)"},
		},
	}
	remediations := buildBaselineRemediations(result)
	actions := make(map[string]bool, len(remediations))
	for _, remediation := range remediations {
		actions[remediation.Action] = true
	}
	for _, action := range []string{"inspect_routes", "compare_route_metrics", "verify_tunnel", "verify_mtu", "diagnose_dns"} {
		if !actions[action] {
			t.Fatalf("missing %s in %+v", action, remediations)
		}
	}
}
