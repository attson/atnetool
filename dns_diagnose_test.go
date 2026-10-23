package main

import (
	"net"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

func TestBuildAndParseDNSQuery(t *testing.T) {
	request, requestID, err := buildDNSQuery("example.com", dnsmessage.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	var query dnsmessage.Message
	if err := query.Unpack(request); err != nil {
		t.Fatal(err)
	}
	if query.ID != requestID || len(query.Questions) != 1 || query.Questions[0].Name.String() != "example.com." {
		t.Fatalf("query = %+v", query)
	}
}

func TestDNSRecordFromResource(t *testing.T) {
	name := dnsmessage.MustNewName("example.com.")
	record, ok := dnsRecordFromResource(dnsmessage.Resource{
		Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 120},
		Body:   &dnsmessage.AResource{A: [4]byte{203, 0, 113, 10}},
	})
	if !ok || record.Type != "A" || record.Value != "203.0.113.10" || record.TTL != 120 {
		t.Fatalf("record = %+v, ok = %t", record, ok)
	}
}

func TestCompareDNSDeepResultsDetectsPrivateRewrite(t *testing.T) {
	results := []DNSResolverResult{
		{ID: "system", Transport: "system", Reachable: true, Queries: []DNSQueryResult{{Records: []DNSRecord{{Type: "A", Value: "127.0.0.1"}}}}},
		{ID: "public", Transport: "udp", Reachable: true, Queries: []DNSQueryResult{{Records: []DNSRecord{{Type: "A", Value: "203.0.113.10"}}}}},
		{ID: "doh", Transport: "doh", Reachable: true, Queries: []DNSQueryResult{{AuthenticData: true, Records: []DNSRecord{{Type: "A", Value: "203.0.113.10"}}}}},
	}
	comparison := compareDNSDeepResults(results)
	if !comparison.PrivateRewriteRisk || comparison.SystemMatchesPublic || !comparison.DNSSECObserved {
		t.Fatalf("comparison = %+v", comparison)
	}
}

func TestBuildDNSDeepFindingsDoesNotCallPublicVariantsPollution(t *testing.T) {
	comparison := DNSDeepComparison{
		ReachableResolvers:  5,
		PublicResolvers:     4,
		SystemAvailable:     true,
		UDPAvailable:        true,
		DoHAvailable:        true,
		SystemMatchesPublic: true,
		PublicAddressSets:   2,
		AddressDifference:   true,
	}
	findings, _ := buildDNSDeepFindings(comparison, make([]DNSResolverResult, 5))
	for _, finding := range findings {
		if finding.Code == "public_variants" && finding.Status != "neutral" {
			t.Fatalf("public variants should be neutral: %+v", finding)
		}
	}
}

func TestBuildDNSDeepFindingsReportsPartialResolverFailure(t *testing.T) {
	comparison := DNSDeepComparison{SystemAvailable: true, UDPAvailable: true, DoHAvailable: true, PublicResolvers: 3}
	resolvers := []DNSResolverResult{
		{Name: "系统 DNS", Transport: "system", Reachable: true},
		{Name: "Cloudflare DNS", Transport: "udp", Reachable: false},
		{Name: "AliDNS", Transport: "udp", Reachable: true},
	}
	findings, remediations := buildDNSDeepFindings(comparison, resolvers)
	found := false
	for _, finding := range findings {
		if finding.Code == "partial_resolver_failure" && finding.Status == "warning" {
			found = true
		}
	}
	if !found || len(remediations) == 0 {
		t.Fatalf("findings = %+v, remediations = %+v", findings, remediations)
	}
}

func TestResolverAddressValuesDeduplicatesAndSorts(t *testing.T) {
	resolver := DNSResolverResult{Queries: []DNSQueryResult{{Records: []DNSRecord{
		{Type: "AAAA", Value: "2001:db8::1"},
		{Type: "A", Value: "203.0.113.10"},
		{Type: "A", Value: "203.0.113.10"},
		{Type: "NS", Value: "ns1.example.com"},
	}}}}
	values := resolverAddressValues(resolver)
	if len(values) != 2 || net.ParseIP(values[0]) == nil || net.ParseIP(values[1]) == nil {
		t.Fatalf("values = %+v", values)
	}
}
