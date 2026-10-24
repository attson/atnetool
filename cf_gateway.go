package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	cfGatewayDialTimeout = 8 * time.Second
	cfGatewayErrorHeader = "X-ATNetool-Gateway-Error"
)

type cfGatewayConfig struct {
	Domain               string
	PreferredIP          string
	ManagedUpstream      *url.URL
	DefaultHTTPUpstream  *url.URL
	DefaultHTTPSUpstream *url.URL
}

type cfGateway struct {
	config   cfGatewayConfig
	listener net.Listener
	server   *http.Server
	requests atomic.Uint64
	closed   atomic.Bool
}

func startCFGateway(config cfGatewayConfig) (*cfGateway, error) {
	domain := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(config.Domain), "."))
	if domain == "" || net.ParseIP(domain) != nil {
		return nil, fmt.Errorf("网关目标域名无效")
	}
	preferredIP := net.ParseIP(strings.TrimSpace(config.PreferredIP))
	if preferredIP == nil {
		return nil, fmt.Errorf("网关优选 IP 无效")
	}
	config.Domain = domain
	config.PreferredIP = preferredIP.String()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("启动本地兼容网关失败：%w", err)
	}
	gateway := &cfGateway{config: config, listener: listener}
	if gateway.upstreamIsSelf(config.ManagedUpstream) || gateway.upstreamIsSelf(config.DefaultHTTPUpstream) || gateway.upstreamIsSelf(config.DefaultHTTPSUpstream) {
		_ = listener.Close()
		return nil, fmt.Errorf("检测到本地网关代理环路，未修改系统代理")
	}
	gateway.server = &http.Server{
		Handler:           gateway,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	go func() {
		if serveErr := gateway.server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			gateway.closed.Store(true)
		}
	}()
	return gateway, nil
}

func (gateway *cfGateway) Endpoint() string {
	if gateway == nil || gateway.listener == nil {
		return ""
	}
	return gateway.listener.Addr().String()
}

func (gateway *cfGateway) URL() string {
	if endpoint := gateway.Endpoint(); endpoint != "" {
		return "http://" + endpoint
	}
	return ""
}

func (gateway *cfGateway) RequestCount() uint64 {
	if gateway == nil {
		return 0
	}
	return gateway.requests.Load()
}

func (gateway *cfGateway) Close(ctx context.Context) error {
	if gateway == nil || gateway.server == nil || gateway.closed.Swap(true) {
		return nil
	}
	return gateway.server.Shutdown(ctx)
}

func (gateway *cfGateway) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	gateway.requests.Add(1)
	if request.Method == http.MethodConnect {
		gateway.handleConnect(writer, request)
		return
	}
	gateway.handleHTTP(writer, request)
}

func (gateway *cfGateway) handleConnect(writer http.ResponseWriter, request *http.Request) {
	host, port, err := gateway.destination(request.Host, "443")
	if err != nil {
		writeCFGatewayError(writer, "Invalid CONNECT target", http.StatusBadRequest)
		return
	}
	managed := gateway.manages(host)
	address := net.JoinHostPort(host, port)
	upstream := gateway.config.DefaultHTTPSUpstream
	if managed {
		address = net.JoinHostPort(gateway.config.PreferredIP, port)
		upstream = gateway.config.ManagedUpstream
	}
	if gateway.isSelfEndpoint(address) {
		writeCFGatewayError(writer, "Proxy loop blocked", http.StatusLoopDetected)
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), cfGatewayDialTimeout)
	remote, err := dialCDNEndpoint(ctx, upstream, address)
	cancel()
	if err != nil {
		writeCFGatewayError(writer, "Gateway connection failed", http.StatusBadGateway)
		return
	}

	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		_ = remote.Close()
		writeCFGatewayError(writer, "Connection tunnelling is unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = remote.Close()
		return
	}
	if _, err = buffered.WriteString("HTTP/1.1 200 Connection Established\r\nProxy-Agent: ATNetool\r\n\r\n"); err != nil {
		_ = client.Close()
		_ = remote.Close()
		return
	}
	if err = buffered.Flush(); err != nil {
		_ = client.Close()
		_ = remote.Close()
		return
	}
	bufferedClient := &bufferedProxyConn{Conn: client, reader: buffered.Reader}
	relayConnections(bufferedClient, remote)
}

func (gateway *cfGateway) handleHTTP(writer http.ResponseWriter, request *http.Request) {
	outgoing := request.Clone(request.Context())
	outgoing.RequestURI = ""
	if outgoing.URL.Scheme == "" {
		outgoing.URL.Scheme = "http"
	}
	if outgoing.URL.Host == "" {
		outgoing.URL.Host = request.Host
	}
	host := outgoing.URL.Hostname()
	if host == "" {
		writeCFGatewayError(writer, "Invalid proxy target", http.StatusBadRequest)
		return
	}
	removeHopHeaders(outgoing.Header)
	outgoing.Header.Del("Proxy-Authorization")

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = false
	transport.MaxIdleConns = 32
	transport.IdleConnTimeout = 60 * time.Second
	if gateway.manages(host) {
		port := outgoing.URL.Port()
		if port == "" {
			if outgoing.URL.Scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		parsedPort, _ := strconv.Atoi(port)
		configureCDNCandidateTransport(transport, gateway.config.ManagedUpstream, gateway.config.PreferredIP, parsedPort)
	} else {
		upstream := gateway.config.DefaultHTTPUpstream
		if outgoing.URL.Scheme == "https" {
			upstream = gateway.config.DefaultHTTPSUpstream
		}
		transport.Proxy = proxyFunction(upstream)
	}
	defer transport.CloseIdleConnections()
	response, err := transport.RoundTrip(outgoing)
	if err != nil {
		writeCFGatewayError(writer, "Gateway request failed", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	removeHopHeaders(response.Header)
	copyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
}

func (gateway *cfGateway) manages(host string) bool {
	return strings.EqualFold(strings.TrimSuffix(host, "."), gateway.config.Domain)
}

func (gateway *cfGateway) destination(rawHost, defaultPort string) (string, string, error) {
	rawHost = strings.TrimSpace(rawHost)
	if rawHost == "" {
		return "", "", fmt.Errorf("empty target")
	}
	host, port, err := net.SplitHostPort(rawHost)
	if err != nil {
		if strings.Contains(rawHost, ":") && net.ParseIP(strings.Trim(rawHost, "[]")) == nil {
			return "", "", err
		}
		host, port = strings.Trim(rawHost, "[]"), defaultPort
	}
	if net.ParseIP(host) == nil {
		host = strings.ToLower(strings.TrimSuffix(host, "."))
	}
	parsedPort, err := strconv.Atoi(port)
	if err != nil || parsedPort < 1 || parsedPort > 65535 {
		return "", "", fmt.Errorf("invalid port")
	}
	return host, port, nil
}

func (gateway *cfGateway) isSelfEndpoint(address string) bool {
	endpoint := gateway.Endpoint()
	if endpoint == "" {
		return false
	}
	return address == endpoint || address == "localhost:"+strings.TrimPrefix(endpoint, "127.0.0.1:")
}

func (gateway *cfGateway) upstreamIsSelf(upstream *url.URL) bool {
	if upstream == nil {
		return false
	}
	host := upstream.Hostname()
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || (!ip.IsLoopback() && !ip.IsUnspecified())) {
		return false
	}
	_, endpointPort, err := net.SplitHostPort(gateway.Endpoint())
	return err == nil && upstream.Port() == endpointPort
}

func writeCFGatewayError(writer http.ResponseWriter, message string, status int) {
	writer.Header().Set(cfGatewayErrorHeader, "1")
	http.Error(writer, message, status)
}

func relayConnections(left, right net.Conn) {
	defer left.Close()
	defer right.Close()
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		_, _ = io.Copy(left, right)
		closeWrite(left)
	}()
	go func() {
		defer wait.Done()
		_, _ = io.Copy(right, left)
		closeWrite(right)
	}()
	wait.Wait()
}

func closeWrite(connection net.Conn) {
	if closer, ok := connection.(interface{ CloseWrite() error }); ok {
		_ = closer.CloseWrite()
	}
}

func removeHopHeaders(header http.Header) {
	if header == nil {
		return
	}
	for _, value := range strings.Split(header.Get("Connection"), ",") {
		header.Del(strings.TrimSpace(value))
	}
	for _, name := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate",
		"Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		header.Del(name)
	}
}

func copyHeaders(destination, source http.Header) {
	for name, values := range source {
		for _, value := range values {
			destination.Add(name, value)
		}
	}
}
