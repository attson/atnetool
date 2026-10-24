package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

const (
	cdnRouteAuto   = "auto"
	cdnRouteDirect = "direct"
	cdnRouteProxy  = "proxy"
	cdnRouteTunnel = "tunnel"
)

func configureCDNRoute(options CDNOptimizeOptions, settings *cdnOptimizeSettings, result *CDNOptimizeResult) error {
	requested := strings.ToLower(strings.TrimSpace(options.RouteMode))
	if requested == "" {
		requested = cdnRouteAuto
	}
	if requested != cdnRouteAuto && requested != cdnRouteDirect && requested != cdnRouteProxy {
		return fmt.Errorf("不支持的候选测试出口")
	}
	result.RequestedRoute = requested

	rawProxy := strings.TrimSpace(options.UpstreamProxyURL)
	if rawProxy == "" {
		rawProxy = result.ProxyCompatibility.SuggestedProxyURL
	}
	useProxy := requested == cdnRouteProxy || (requested == cdnRouteAuto && result.ProxyCompatibility.RecommendedRoute == cdnRouteProxy)
	if useProxy {
		if rawProxy == "" {
			return fmt.Errorf("没有检测到可用于兼容优选的 HTTP / SOCKS 代理")
		}
		proxyURL, err := validateProxyURL(rawProxy)
		if err != nil {
			return fmt.Errorf("上游代理地址配置有误：%w", err)
		}
		settings.UpstreamProxy = proxyURL
		result.EffectiveRoute = cdnRouteProxy
		result.UpstreamProxyURL = safeProxyURL(proxyURL)
		return nil
	}

	if result.ProxyCompatibility.TunnelDetected {
		result.EffectiveRoute = cdnRouteTunnel
	} else {
		result.EffectiveRoute = cdnRouteDirect
	}
	return nil
}

func preserveSystemProxyCredentials(rawProxy, targetURL string, compatibility ProxyCompatibility) string {
	rawProxy = strings.TrimSpace(rawProxy)
	request, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		return rawProxy
	}
	configured, err := systemProxyForRequest(request)
	if err != nil || configured == nil {
		return rawProxy
	}
	safeConfigured := safeProxyURL(configured)
	if rawProxy == "" || rawProxy == compatibility.SystemProxyURL || rawProxy == safeConfigured {
		return configured.String()
	}
	return rawProxy
}

func cdnRouteEnvironmentDiagnosis(result CDNOptimizeResult, clientIP string) DiagResult {
	mode := proxyModeDirect
	if result.EffectiveRoute == cdnRouteProxy {
		mode = proxyModeManual
	}
	return DiagResult{ProxyMode: mode, ProxyURL: result.UpstreamProxyURL, ClientIP: clientIP}
}

func applyCDNRouteGovernance(result *CDNOptimizeResult) {
	if result == nil || result.BestIP == "" {
		return
	}
	switch result.EffectiveRoute {
	case cdnRouteProxy:
		result.Governance = []CDNGovernanceAdvice{
			{Action: "gateway_apply", Title: "可通过内置兼容网关应用", Detail: "CF 请求将经本次代理出口连接推荐 IP，其他流量继续转发到原系统代理。停止后自动恢复原设置。"},
			{Action: "direct_baseline", Title: "应用直连治理前重新测试", Detail: "若要生成 hosts、mosdns 或 3Proxy 配置，请切换到本机网络出口后使用相同参数复测。"},
			{Action: "retest", Title: "在高峰时段复测", Detail: "代理规则和上游节点可能改变实际路径，建议保存结果并观察多次排名稳定性。"},
		}
	case cdnRouteTunnel:
		result.Governance = append([]CDNGovernanceAdvice{{
			Action: "gateway_apply", Title: "可通过内置兼容网关应用", Detail: "网关只修改系统 HTTP / HTTPS 代理；实际连接仍经过当前 TUN，停止后自动恢复原设置。",
		}, {
			Action: "tun_scope", Title: "先确认 TUN 与 Fake-IP 规则", Detail: "当前结果来自 TUN 路径；hosts 或本地 DNS 是否生效取决于代理软件的 DNS 与路由规则。",
		}}, result.Governance...)
	default:
		result.Governance = append([]CDNGovernanceAdvice{{
			Action: "gateway_apply", Title: "可通过内置兼容网关应用", Detail: "网关只将当前域名连接到推荐 IP，其他流量保持原出口；停止后自动恢复系统代理。",
		}}, result.Governance...)
	}
}

func proxyFunction(upstream *url.URL) func(*http.Request) (*url.URL, error) {
	if upstream == nil {
		return nil
	}
	return http.ProxyURL(upstream)
}

func configureCDNCandidateTransport(transport *http.Transport, upstream *url.URL, candidateIP string, port int) {
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		targetPort := port
		if targetPort <= 0 {
			_, rawPort, splitErr := net.SplitHostPort(address)
			if splitErr == nil {
				targetPort, _ = strconv.Atoi(rawPort)
			}
		}
		if targetPort <= 0 {
			targetPort = 443
		}
		target := net.JoinHostPort(candidateIP, strconv.Itoa(targetPort))
		return dialCDNEndpoint(ctx, upstream, target)
	}
}

func dialCDNEndpoint(ctx context.Context, upstream *url.URL, address string) (net.Conn, error) {
	if upstream == nil {
		return (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}).DialContext(ctx, "tcp", address)
	}
	switch strings.ToLower(upstream.Scheme) {
	case "socks5", "socks5h":
		base := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}
		dialer, err := xproxy.FromURL(upstream, base)
		if err != nil {
			return nil, err
		}
		if contextDialer, ok := dialer.(xproxy.ContextDialer); ok {
			return contextDialer.DialContext(ctx, "tcp", address)
		}
		return dialer.Dial("tcp", address)
	case "http", "https":
		return dialHTTPProxyTunnel(ctx, upstream, address)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", upstream.Scheme)
	}
}

func dialHTTPProxyTunnel(ctx context.Context, upstream *url.URL, address string) (net.Conn, error) {
	proxyAddress := upstream.Host
	if upstream.Port() == "" {
		defaultPort := "80"
		if upstream.Scheme == "https" {
			defaultPort = "443"
		}
		proxyAddress = net.JoinHostPort(upstream.Hostname(), defaultPort)
	}
	connection, err := (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: -1}).DialContext(ctx, "tcp", proxyAddress)
	if err != nil {
		return nil, err
	}
	if upstream.Scheme == "https" {
		tlsConnection := tls.Client(connection, &tls.Config{ServerName: upstream.Hostname(), MinVersion: tls.VersionTLS12})
		if err := tlsConnection.HandshakeContext(ctx); err != nil {
			_ = connection.Close()
			return nil, err
		}
		connection = tlsConnection
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = connection.SetDeadline(deadline)
		defer connection.SetDeadline(time.Time{})
	}
	request := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: address},
		Host:   address,
		Header: make(http.Header),
	}
	if upstream.User != nil {
		password, _ := upstream.User.Password()
		token := base64.StdEncoding.EncodeToString([]byte(upstream.User.Username() + ":" + password))
		request.Header.Set("Proxy-Authorization", "Basic "+token)
	}
	if err := request.Write(connection); err != nil {
		_ = connection.Close()
		return nil, err
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		_ = response.Body.Close()
		_ = connection.Close()
		return nil, fmt.Errorf("proxy CONNECT returned %s", response.Status)
	}
	return &bufferedProxyConn{Conn: connection, reader: reader}, nil
}

type bufferedProxyConn struct {
	net.Conn
	reader *bufio.Reader
}

func (connection *bufferedProxyConn) Read(buffer []byte) (int, error) {
	return connection.reader.Read(buffer)
}
