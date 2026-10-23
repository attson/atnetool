//go:build windows

package main

import (
	"net"
	"sort"
	"unsafe"

	"golang.org/x/sys/windows"
)

func detectDefaultRoutes() []DefaultRoute {
	routes := make([]DefaultRoute, 0, 4)
	adapters := windowsAdaptersWithGateways()
	selected := make(map[string]DefaultRoute, 2)
	for _, candidate := range []struct{ family, network, endpoint string }{
		{"IPv4", "udp4", "1.1.1.1:53"},
		{"IPv6", "udp6", "[2606:4700:4700::1111]:53"},
	} {
		connection, err := net.Dial(candidate.network, candidate.endpoint)
		if err != nil {
			continue
		}
		localIP := addressIP(connection.LocalAddr())
		_ = connection.Close()
		interfaceName := interfaceForIP(localIP)
		route := DefaultRoute{Family: candidate.family, Interface: interfaceName, LocalIP: localIP, Selected: true}
		if adapter, found := adapters[interfaceName]; found {
			route.Gateway = adapter.gateway(candidate.family)
			if candidate.family == "IPv6" {
				route.Metric = int(adapter.ipv6Metric)
			} else {
				route.Metric = int(adapter.ipv4Metric)
			}
		}
		selected[candidate.family] = route
	}
	for interfaceName, adapter := range adapters {
		for _, gateway := range adapter.gateways {
			family := "IPv6"
			metric := int(adapter.ipv6Metric)
			if gateway.To4() != nil {
				family = "IPv4"
				metric = int(adapter.ipv4Metric)
			}
			route := DefaultRoute{Family: family, Interface: interfaceName, Gateway: gateway.String(), Metric: metric}
			if active, found := selected[family]; found && active.Interface == interfaceName {
				route.LocalIP = active.LocalIP
				route.Selected = true
			}
			routes = append(routes, route)
		}
	}
	for family, active := range selected {
		found := false
		for _, route := range routes {
			if route.Family == family && route.Interface == active.Interface {
				found = true
				break
			}
		}
		if !found {
			routes = append(routes, active)
		}
	}
	sort.Slice(routes, func(left, right int) bool {
		if routes[left].Family != routes[right].Family {
			return routes[left].Family < routes[right].Family
		}
		if routes[left].Selected != routes[right].Selected {
			return routes[left].Selected
		}
		return routes[left].Metric < routes[right].Metric
	})
	return routes
}

type windowsAdapterRoute struct {
	gateways   []net.IP
	ipv4Metric uint32
	ipv6Metric uint32
}

func (route windowsAdapterRoute) gateway(family string) string {
	for _, gateway := range route.gateways {
		if (family == "IPv4") == (gateway.To4() != nil) {
			return gateway.String()
		}
	}
	return ""
}

func windowsAdaptersWithGateways() map[string]windowsAdapterRoute {
	result := make(map[string]windowsAdapterRoute)
	size := uint32(15 * 1024)
	for attempts := 0; attempts < 3; attempts++ {
		buffer := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buffer[0]))
		err := windows.GetAdaptersAddresses(
			windows.AF_UNSPEC,
			windows.GAA_FLAG_INCLUDE_GATEWAYS|windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST,
			0,
			first,
			&size,
		)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue
		}
		if err != nil {
			return result
		}
		for adapter := first; adapter != nil; adapter = adapter.Next {
			if adapter.OperStatus != windows.IfOperStatusUp {
				continue
			}
			name := windows.UTF16PtrToString(adapter.FriendlyName)
			entry := windowsAdapterRoute{ipv4Metric: adapter.Ipv4Metric, ipv6Metric: adapter.Ipv6Metric}
			for gateway := adapter.FirstGatewayAddress; gateway != nil; gateway = gateway.Next {
				if address := gateway.Address.IP(); address != nil && !address.IsUnspecified() {
					entry.gateways = append(entry.gateways, append(net.IP(nil), address...))
				}
			}
			result[name] = entry
		}
		return result
	}
	return result
}
