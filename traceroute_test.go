package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseTracepathOutput(t *testing.T) {
	t.Parallel()

	output := ` 1?: [LOCALHOST]                      pmtu 1500
 1:  192.168.1.1                                           1.250ms
 1:  192.168.1.1                                           0.750ms
 2:  no reply
 3:  203.0.113.8                                          12.500ms reached
`
	hops := parseTracerouteOutput(output, []string{"203.0.113.8"})
	if len(hops) != 3 {
		t.Fatalf("len(hops) = %d, want 3: %+v", len(hops), hops)
	}
	if hops[0].Hop != 1 || len(hops[0].RTTMS) != 2 || hops[0].TimedOut {
		t.Errorf("first hop = %+v", hops[0])
	}
	if !hops[1].TimedOut || len(hops[1].Addresses) != 0 {
		t.Errorf("timeout hop = %+v", hops[1])
	}
	if !hops[2].Reached || !traceReached(hops) {
		t.Errorf("target hop = %+v", hops[2])
	}
}

func TestParseUnixTracerouteOutput(t *testing.T) {
	t.Parallel()

	output := `traceroute to example.com (93.184.216.34), 15 hops max
 1  192.168.0.1  0.431 ms
 2  *
 3  93.184.216.34  8.420 ms
`
	hops := parseTracerouteOutput(output, []string{"93.184.216.34"})
	if len(hops) != 3 || !hops[1].TimedOut || !hops[2].Reached {
		t.Fatalf("unexpected hops: %+v", hops)
	}
}

func TestParseWindowsTracertOutput(t *testing.T) {
	t.Parallel()

	output := `
  1    <1 ms    <1 ms     1 ms  192.168.1.1
  2     *        *        *     Request timed out.
  3    12 ms    11 ms    13 ms  1.1.1.1
`
	hops := parseTracerouteOutput(output, []string{"1.1.1.1"})
	if len(hops) != 3 || len(hops[0].RTTMS) != 3 || !hops[1].TimedOut || !hops[2].Reached {
		t.Fatalf("unexpected hops: %+v", hops)
	}
}

func TestLookupTraceGeo(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/8.8.8.8" {
			t.Errorf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"ip":"8.8.8.8","success":true,"country":"United States","country_code":"US",
			"region":"California","city":"Mountain View","latitude":37.4056,"longitude":-122.0775,
			"connection":{"asn":15169,"org":"Google LLC"}
		}`))
	}))
	defer server.Close()

	geo, err := lookupTraceGeo(context.Background(), server.Client(), server.URL, "8.8.8.8")
	if err != nil {
		t.Fatalf("lookupTraceGeo() error = %v", err)
	}
	if geo.City != "Mountain View" || geo.CountryCode != "US" || geo.ASN != 15169 {
		t.Fatalf("lookupTraceGeo() = %+v", geo)
	}
}

func TestLookupTraceGeoAlternative(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/8.8.8.8/json" {
			t.Errorf("path = %q", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"ip":"8.8.8.8","city":"Mountain View","region":"California",
			"country":"US","loc":"37.386,-122.084","org":"AS15169 Google LLC"
		}`))
	}))
	defer server.Close()

	geo, err := lookupTraceGeoAlternative(context.Background(), server.Client(), server.URL, "8.8.8.8")
	if err != nil {
		t.Fatalf("lookupTraceGeoAlternative() error = %v", err)
	}
	if geo.City != "Mountain View" || geo.ASN != 15169 || geo.Source != "ipinfo.io" {
		t.Fatalf("lookupTraceGeoAlternative() = %+v", geo)
	}
}

func TestMergeTraceGeoConfidence(t *testing.T) {
	t.Parallel()

	primary := TraceGeo{
		IP: "8.8.8.8", City: "Mountain View", CountryCode: "US",
		Latitude: 37.4056, Longitude: -122.0775, Source: "ipwho.is",
	}
	nearby := TraceGeo{
		IP: "8.8.8.8", City: "Mountain View", Region: "California", CountryCode: "US",
		Latitude: 37.386, Longitude: -122.084, ASN: 15169, Organisation: "Google LLC", Source: "ipinfo.io",
	}
	merged := mergeTraceGeo(primary, nearby)
	if merged.Confidence != "high" || merged.Source != "ipwho.is + ipinfo.io" || merged.ASN != 15169 {
		t.Fatalf("mergeTraceGeo(nearby) = %+v", merged)
	}

	distant := nearby
	distant.City = "New York"
	distant.Latitude = 40.7128
	distant.Longitude = -74.0060
	if got := mergeTraceGeo(primary, distant); got.Confidence != "low" {
		t.Fatalf("mergeTraceGeo(distant).Confidence = %q, want low", got.Confidence)
	}
}

func TestLookupTraceGeoConsensusFallsBack(t *testing.T) {
	t.Parallel()

	primary := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Error(writer, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"ip":"1.1.1.1","city":"Sydney","region":"New South Wales",
			"country":"AU","loc":"-33.8688,151.2093","org":"AS13335 Cloudflare"
		}`))
	}))
	defer secondary.Close()

	geo, err := lookupTraceGeoConsensus(context.Background(), secondary.Client(), primary.URL, secondary.URL, "1.1.1.1")
	if err != nil {
		t.Fatalf("lookupTraceGeoConsensus() error = %v", err)
	}
	if geo.City != "Sydney" || geo.Source != "ipinfo.io" || geo.Confidence != "estimated" {
		t.Fatalf("lookupTraceGeoConsensus() = %+v", geo)
	}
}

func TestIsPublicTraceIP(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"10.0.0.1", "172.16.0.1", "192.168.1.1", "127.0.0.1", "::1", "fe80::1"} {
		if isPublicTraceIP(address) {
			t.Errorf("isPublicTraceIP(%q) = true, want false", address)
		}
	}
	for _, address := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !isPublicTraceIP(address) {
			t.Errorf("isPublicTraceIP(%q) = false, want true", address)
		}
	}
}
