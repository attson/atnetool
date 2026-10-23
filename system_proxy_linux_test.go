//go:build linux

package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLinuxSystemProxyReadsGnomeManualSettings(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "https://aic.dowte.com", nil)
	proxyURL, err := linuxSystemProxyForRequest(request, fakeGSettings(map[string]string{
		gnomeProxySchema + "/mode":                   "'manual'",
		gnomeProxySchema + "/ignore-hosts":           "['localhost', '127.*', '*.local']",
		gnomeProxySchema + "/use-same-proxy":         "true",
		gnomeHTTPProxySchema + "/host":               "'127.0.0.1'",
		gnomeHTTPProxySchema + "/port":               "7890",
		gnomeHTTPProxySchema + "/use-authentication": "false",
	}))
	if err != nil {
		t.Fatalf("linuxSystemProxyForRequest() error = %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy URL = %v, want http://127.0.0.1:7890", proxyURL)
	}
}

func TestLinuxSystemProxyHonoursIgnoreHosts(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "https://service.local", nil)
	proxyURL, err := linuxSystemProxyForRequest(request, fakeGSettings(map[string]string{
		gnomeProxySchema + "/mode":           "'manual'",
		gnomeProxySchema + "/ignore-hosts":   "['localhost', '*.local']",
		gnomeProxySchema + "/use-same-proxy": "true",
		gnomeHTTPProxySchema + "/host":       "'127.0.0.1'",
		gnomeHTTPProxySchema + "/port":       "7890",
	}))
	if err != nil {
		t.Fatalf("linuxSystemProxyForRequest() error = %v", err)
	}
	if proxyURL != nil {
		t.Fatalf("ignored host returned proxy %v", proxyURL)
	}
}

func TestLinuxSystemProxyReportsUnsupportedPAC(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "https://example.com", nil)
	_, err := linuxSystemProxyForRequest(request, fakeGSettings(map[string]string{
		gnomeProxySchema + "/mode": "'auto'",
	}))
	if err == nil {
		t.Fatal("linuxSystemProxyForRequest() accepted unsupported PAC mode")
	}
}

func fakeGSettings(values map[string]string) gsettingsReader {
	return func(schema, key string) (string, error) {
		value, ok := values[schema+"/"+key]
		if !ok {
			return "", errors.New("setting is unavailable")
		}
		return value, nil
	}
}
