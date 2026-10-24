package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestCFGatewayRoutesManagedHTTPHostToPreferredIP(t *testing.T) {
	var observedHost string
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedHost = request.Host
		writer.Header().Set("CF-Ray", "gateway-test-HKG")
		_, _ = writer.Write([]byte("managed"))
	}))
	defer origin.Close()
	originURL, _ := url.Parse(origin.URL)

	gateway, err := startCFGateway(cfGatewayConfig{Domain: "edge.example", PreferredIP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close(context.Background())
	gatewayURL, _ := url.Parse(gateway.URL())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(gatewayURL)}, Timeout: 3 * time.Second}
	response, err := client.Get("http://edge.example:" + originURL.Port() + "/test")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "managed" || observedHost != "edge.example:"+originURL.Port() {
		t.Fatalf("body=%q host=%q", body, observedHost)
	}
}

func TestCFGatewayForwardsUnmanagedHTTPNormally(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("default"))
	}))
	defer origin.Close()
	gateway, err := startCFGateway(cfGatewayConfig{Domain: "edge.example", PreferredIP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close(context.Background())
	gatewayURL, _ := url.Parse(gateway.URL())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(gatewayURL)}, Timeout: 3 * time.Second}
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "default" {
		t.Fatalf("body=%q", body)
	}
}

func TestCFGatewayPreservesDefaultHTTPUpstream(t *testing.T) {
	observed := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed <- request.URL.Host
		_, _ = writer.Write([]byte("through-http-upstream"))
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	gateway, err := startCFGateway(cfGatewayConfig{
		Domain: "edge.example", PreferredIP: "127.0.0.1", DefaultHTTPUpstream: upstreamURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close(context.Background())
	gatewayURL, _ := url.Parse(gateway.URL())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(gatewayURL)}, Timeout: 3 * time.Second}
	response, err := client.Get("http://unmanaged.example/path")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "through-http-upstream" {
		t.Fatalf("body=%q", body)
	}
	if host := <-observed; host != "unmanaged.example" {
		t.Fatalf("upstream target=%q", host)
	}
}

func TestCFGatewayPreservesDefaultHTTPSUpstreamForConnect(t *testing.T) {
	observed := make(chan string, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observed <- request.Host
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Error("upstream proxy does not support hijacking")
			return
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	gateway, err := startCFGateway(cfGatewayConfig{
		Domain: "edge.example", PreferredIP: "127.0.0.1", DefaultHTTPSUpstream: upstreamURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close(context.Background())

	connection, err := net.DialTimeout("tcp", gateway.Endpoint(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, _ = fmt.Fprint(connection, "CONNECT unmanaged.example:443 HTTP/1.1\r\nHost: unmanaged.example:443\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%s", response.Status)
	}
	if target := <-observed; target != "unmanaged.example:443" {
		t.Fatalf("upstream CONNECT target=%q", target)
	}
}

func TestCFGatewayUsesManagedUpstreamWithoutLosingPreferredIP(t *testing.T) {
	type observation struct{ connectTarget, host string }
	observed := make(chan observation, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			t.Error("upstream proxy does not support hijacking")
			return
		}
		connection, buffered, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffered.Flush()
		tunneled, err := http.ReadRequest(bufio.NewReader(connection))
		if err != nil {
			t.Error(err)
			return
		}
		observed <- observation{connectTarget: request.Host, host: tunneled.Host}
		_, _ = fmt.Fprint(connection, "HTTP/1.1 204 No Content\r\nContent-Length: 0\r\nConnection: close\r\n\r\n")
	}))
	defer upstream.Close()
	upstreamURL, _ := url.Parse(upstream.URL)
	gateway, err := startCFGateway(cfGatewayConfig{
		Domain: "edge.example", PreferredIP: "104.16.1.20", ManagedUpstream: upstreamURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close(context.Background())
	gatewayURL, _ := url.Parse(gateway.URL())
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(gatewayURL)}, Timeout: 3 * time.Second}
	response, err := client.Get("http://edge.example/")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	got := <-observed
	if got.connectTarget != "104.16.1.20:80" || got.host != "edge.example" {
		t.Fatalf("observation=%+v", got)
	}
}

func TestCFGatewayConnectUsesPreferredIP(t *testing.T) {
	origin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer origin.Close()
	go func() {
		connection, acceptErr := origin.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
	}()

	gateway, err := startCFGateway(cfGatewayConfig{Domain: "edge.example", PreferredIP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close(context.Background())
	connection, err := net.DialTimeout("tcp", gateway.Endpoint(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_, port, _ := net.SplitHostPort(origin.Addr().String())
	_, _ = fmt.Fprintf(connection, "CONNECT edge.example:%s HTTP/1.1\r\nHost: edge.example:%s\r\n\r\n", port, port)
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%s", response.Status)
	}
	_ = connection.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = connection.Write([]byte("ping"))
	buffer := make([]byte, 4)
	if _, err = io.ReadFull(reader, buffer); err != nil || string(buffer) != "ping" {
		t.Fatalf("echo=%q err=%v", buffer, err)
	}
}

func TestCFGatewayDetectsLoopbackSelfUpstream(t *testing.T) {
	gateway, err := startCFGateway(cfGatewayConfig{Domain: "edge.example", PreferredIP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer gateway.Close(context.Background())

	_, port, err := net.SplitHostPort(gateway.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	for _, rawURL := range []string{
		"http://127.0.0.1:" + port,
		"http://localhost:" + port,
		"http://[::1]:" + port,
		"http://0.0.0.0:" + port,
	} {
		upstream, parseErr := url.Parse(rawURL)
		if parseErr != nil {
			t.Fatal(parseErr)
		}
		if !gateway.upstreamIsSelf(upstream) {
			t.Errorf("expected %s to be detected as the gateway itself", rawURL)
		}
	}

	otherPort := "1"
	if port == otherPort {
		otherPort = "2"
	}
	upstream, _ := url.Parse("http://127.0.0.1:" + otherPort)
	if gateway.upstreamIsSelf(upstream) {
		t.Fatal("different loopback port must not be treated as the gateway itself")
	}
}
