package main

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

type NetworkInterfaceSnapshot struct {
	Name         string   `json:"name"`
	Index        int      `json:"index"`
	MTU          int      `json:"mtu"`
	Hardware     string   `json:"hardware,omitempty"`
	Addresses    []string `json:"addresses,omitempty"`
	AddressTypes []string `json:"address_types,omitempty"`
	Flags        []string `json:"flags,omitempty"`
	Type         string   `json:"type"`
	Active       bool     `json:"active"`
	DefaultRoute bool     `json:"default_route"`
	Virtual      bool     `json:"virtual"`
	Tunnel       bool     `json:"tunnel"`
	HasIPv4      bool     `json:"has_ipv4"`
	HasIPv6      bool     `json:"has_ipv6"`
	MTURisk      bool     `json:"mtu_risk"`
}

type NetworkEnvironment struct {
	CapturedAt string                     `json:"captured_at"`
	OS         string                     `json:"os"`
	Arch       string                     `json:"arch"`
	Hostname   string                     `json:"hostname,omitempty"`
	DNSServers []string                   `json:"dns_servers,omitempty"`
	Interfaces []NetworkInterfaceSnapshot `json:"interfaces,omitempty"`
	ProxyMode  string                     `json:"proxy_mode"`
	ProxyURL   string                     `json:"proxy_url,omitempty"`
}

func captureNetworkEnvironment(diagnosis DiagResult) NetworkEnvironment {
	environment := NetworkEnvironment{
		CapturedAt: time.Now().Format(time.RFC3339),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		ProxyMode:  diagnosis.ProxyMode,
		ProxyURL:   diagnosis.ProxyURL,
		DNSServers: readResolverAddresses(),
	}
	environment.Hostname, _ = os.Hostname()
	interfaces, err := net.Interfaces()
	if err != nil {
		return environment
	}
	for _, item := range interfaces {
		if item.Flags&net.FlagUp == 0 || item.Flags&net.FlagLoopback != 0 {
			continue
		}
		snapshot := NetworkInterfaceSnapshot{
			Name:     item.Name,
			Index:    item.Index,
			MTU:      item.MTU,
			Hardware: item.HardwareAddr.String(),
			Flags:    interfaceFlagNames(item.Flags),
		}
		snapshot.Type, snapshot.Virtual, snapshot.Tunnel = classifyNetworkInterface(item.Name, snapshot.Hardware)
		addresses, _ := item.Addrs()
		for _, address := range addresses {
			snapshot.Addresses = append(snapshot.Addresses, address.String())
			if ip := interfaceAddressIP(address.String()); ip != nil {
				if ip.To4() != nil {
					snapshot.HasIPv4 = true
				} else {
					snapshot.HasIPv6 = true
				}
				snapshot.AddressTypes = appendUnique(snapshot.AddressTypes, classifyIPAddress(ip))
			}
			if diagnosis.ClientIP != "" && addressContainsIP(address.String(), diagnosis.ClientIP) {
				snapshot.Active = true
			}
		}
		snapshot.MTURisk = item.MTU > 0 && (item.MTU < 1280 || item.MTU > 9000)
		environment.Interfaces = append(environment.Interfaces, snapshot)
	}
	return environment
}

func interfaceFlagNames(flags net.Flags) []string {
	values := make([]string, 0, 5)
	for _, candidate := range []struct {
		flag net.Flags
		name string
	}{{net.FlagUp, "up"}, {net.FlagBroadcast, "broadcast"}, {net.FlagPointToPoint, "point-to-point"}, {net.FlagMulticast, "multicast"}} {
		if flags&candidate.flag != 0 {
			values = append(values, candidate.name)
		}
	}
	return values
}

func classifyNetworkInterface(name, hardware string) (string, bool, bool) {
	lower := strings.ToLower(strings.TrimSpace(name))
	tunnelPrefixes := []string{"tun", "tap", "utun", "wg", "wintun", "wireguard", "vpn", "ppp", "ipsec", "tailscale", "zerotier", "clash", "mihomo", "nordlynx", "protonvpn", "warp"}
	for _, prefix := range tunnelPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return "tunnel", true, true
		}
	}
	virtualPrefixes := []string{"docker", "veth", "vethernet", "br-", "virbr", "vmnet", "vmware", "vbox", "virtualbox", "hyper-v", "wsl", "podman", "cni", "flannel"}
	for _, prefix := range virtualPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return "virtual", true, false
		}
	}
	if hardware == "" {
		return "logical", true, false
	}
	return "physical", false, false
}

func interfaceAddressIP(address string) net.IP {
	ip, _, err := net.ParseCIDR(address)
	if err == nil {
		return ip
	}
	return net.ParseIP(address)
}

func classifyIPAddress(ip net.IP) string {
	switch {
	case ip == nil:
		return "unknown"
	case ip.IsLoopback():
		return "loopback"
	case ip.IsLinkLocalUnicast():
		return "link-local"
	case ip.IsPrivate():
		return "private"
	case ip.IsUnspecified():
		return "unspecified"
	default:
		return "public"
	}
}

func readResolverAddresses() []string {
	file, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer file.Close()
	var servers []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[0] == "nameserver" && net.ParseIP(fields[1]) != nil {
			servers = appendUnique(servers, fields[1])
		}
	}
	return servers
}

func addressContainsIP(address, expected string) bool {
	host, _, err := net.ParseCIDR(address)
	if err != nil {
		host = net.ParseIP(address)
	}
	wanted := net.ParseIP(expected)
	return host != nil && wanted != nil && host.Equal(wanted)
}
