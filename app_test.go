package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestRunDiagnoseCollectsResponseDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/health", http.StatusFound)
			return
		}
		if got := request.Header.Get("User-Agent"); got != "ATNetool/1.0" {
			t.Errorf("User-Agent = %q", got)
		}
		time.Sleep(5 * time.Millisecond)
		writer.Header().Set("CF-Ray", "test-ray-SJC")
		writer.Header().Set("CF-Cache-Status", "HIT")
		writer.Header().Set("Server", "diagnostic-test")
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("pong"))
	}))
	defer server.Close()

	result := NewApp().RunDiagnose(server.URL + "/start")

	if !result.Success {
		t.Fatalf("RunDiagnose() was not successful: %+v", result)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if result.CFRay != "test-ray-SJC" {
		t.Errorf("CFRay = %q", result.CFRay)
	}
	if result.CFColo != "SJC" || result.CFCacheStatus != "HIT" {
		t.Errorf("Cloudflare details were not captured: %+v", result)
	}
	if result.RedirectCount != 1 || len(result.RedirectChain) != 2 || !strings.HasSuffix(result.FinalURL, "/health") {
		t.Errorf("redirect details were not captured: %+v", result)
	}
	if result.HTTPProtocol == "" || result.ServerHeader != "diagnostic-test" || result.ContentType != "text/plain" {
		t.Errorf("HTTP details were not captured: %+v", result)
	}
	if result.ResponseBytes != 4 || result.ResponseTruncated {
		t.Errorf("response size = %d truncated=%t, want 4 false", result.ResponseBytes, result.ResponseTruncated)
	}
	if result.TCPConnectMS <= 0 {
		t.Errorf("TCPConnectMS = %d, want positive value", result.TCPConnectMS)
	}
	if result.TTFBMS < 5 {
		t.Errorf("TTFBMS = %d, want at least 5", result.TTFBMS)
	}
	if result.TotalMS < result.TTFBMS {
		t.Errorf("TotalMS = %d, want >= TTFBMS %d", result.TotalMS, result.TTFBMS)
	}
	if result.ClientIP == "" || result.RemoteIP == "" {
		t.Errorf("connection addresses were not captured: %+v", result)
	}
	if result.LocalAddress == "" || result.RemoteAddress == "" {
		t.Errorf("detailed connection endpoints were not captured: %+v", result)
	}
}

func TestPopulateTLSDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	certificate := server.Certificate()
	if len(certificate.DNSNames) == 0 {
		t.Fatal("test certificate has no DNS names")
	}

	var result DiagResult
	populateTLSDetails(&result, tls.ConnectionState{
		Version:            tls.VersionTLS13,
		CipherSuite:        tls.TLS_AES_128_GCM_SHA256,
		NegotiatedProtocol: "h2",
		PeerCertificates:   []*x509.Certificate{certificate},
	}, certificate.DNSNames[0], certificate.NotBefore.Add(time.Hour))

	if result.TLSVersion != "TLS 1.3" || result.TLSCipherSuite == "" || result.TLSALPN != "h2" {
		t.Errorf("TLS transport details were not captured: %+v", result)
	}
	if !result.TLSCertValid || result.TLSCertSubject == "" || result.TLSCertExpiresAt == "" {
		t.Errorf("TLS certificate details were not captured: %+v", result)
	}
}

func TestRunDiagnoseCapsLargeResponse(t *testing.T) {
	t.Parallel()

	payload := bytes.Repeat([]byte("x"), maxResponseBody+512)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	result := NewApp().RunDiagnose(server.URL)
	if !result.Success {
		t.Fatalf("RunDiagnose() was not successful: %+v", result)
	}
	if result.ResponseBytes != maxResponseBody || !result.ResponseTruncated {
		t.Fatalf("response size = %d truncated=%t, want %d true", result.ResponseBytes, result.ResponseTruncated, maxResponseBody)
	}
}

func TestRunDiagnoseRejectsUnsupportedURL(t *testing.T) {
	t.Parallel()

	result := NewApp().RunDiagnose("file:///etc/passwd")

	if result.Success {
		t.Fatal("RunDiagnose() unexpectedly succeeded")
	}
	if result.FailedStage != "request" {
		t.Errorf("FailedStage = %q, want request", result.FailedStage)
	}
	if result.Error == "" {
		t.Error("Error is empty")
	}
}

func TestValidateHTTPURLAddsHTTPSForBareDomain(t *testing.T) {
	t.Parallel()

	parsedURL, err := validateHTTPURL(" example.com/health ")
	if err != nil {
		t.Fatalf("validateHTTPURL() error = %v", err)
	}
	if got, want := parsedURL.String(), "https://example.com/health"; got != want {
		t.Errorf("validateHTTPURL() = %q, want %q", got, want)
	}
}

func TestRunDiagnoseWithManualProxy(t *testing.T) {
	t.Parallel()

	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Host != "target.example" {
			t.Errorf("proxy received host %q", request.URL.Host)
		}
		writer.Header().Set("CF-Ray", "proxy-test-ray")
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()

	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxyURL.User = url.UserPassword("diagnose", "secret")
	result := NewApp().RunDiagnoseWithOptions(DiagOptions{
		TargetURL: "http://target.example/health",
		ProxyMode: proxyModeManual,
		ProxyURL:  proxyURL.String(),
	})

	if !result.Success {
		t.Fatalf("RunDiagnoseWithOptions() was not successful: %+v", result)
	}
	if !result.ProxyDetected || result.ProxyStatus != "proxy" {
		t.Errorf("proxy was not reported: %+v", result)
	}
	if strings.Contains(result.ProxyURL, "diagnose") || strings.Contains(result.ProxyURL, "secret") {
		t.Errorf("ProxyURL leaked credentials: %q", result.ProxyURL)
	}
	if result.CFRay != "proxy-test-ray" {
		t.Errorf("CFRay = %q", result.CFRay)
	}
}

func TestProxyForRequestDirectModeBypassesProxy(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	proxyFunc, proxyURL, err := proxyForRequest(proxyModeDirect, "http://127.0.0.1:7890", request)
	if err != nil {
		t.Fatalf("proxyForRequest() error = %v", err)
	}
	if proxyFunc != nil || proxyURL != nil {
		t.Fatalf("direct mode returned proxy function=%t and URL=%v", proxyFunc != nil, proxyURL)
	}
}

func TestValidateProxyURL(t *testing.T) {
	t.Parallel()

	proxyURL, err := validateProxyURL("127.0.0.1:7890")
	if err != nil {
		t.Fatalf("validateProxyURL() error = %v", err)
	}
	if got, want := proxyURL.String(), "http://127.0.0.1:7890"; got != want {
		t.Errorf("validateProxyURL() = %q, want %q", got, want)
	}
	if _, err := validateProxyURL("ftp://127.0.0.1:21"); err == nil {
		t.Error("validateProxyURL() accepted unsupported FTP proxy")
	}
}

func TestDiagnoseFailurePrioritisesNetworkStages(t *testing.T) {
	t.Parallel()

	stage, _ := diagnoseFailure(errors.New("dns"), errors.New("connect"), errors.New("tls"))
	if stage != "dns" {
		t.Errorf("stage = %q, want dns", stage)
	}

	stage, _ = diagnoseFailure(nil, errors.New("connect"), errors.New("tls"))
	if stage != "tls" {
		t.Errorf("stage = %q, want tls", stage)
	}
}
