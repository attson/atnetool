//go:build darwin

package main

import (
	"net"
	"sort"
	"syscall"

	"golang.org/x/net/route"
)

func detectDefaultRoutes() []DefaultRoute {
	routes := make([]DefaultRoute, 0, 4)
	for _, candidate := range []struct {
		family, network, endpoint string
		af                        int
	}{
		{"IPv4", "udp4", "1.1.1.1:53", syscall.AF_INET},
		{"IPv6", "udp6", "[2606:4700:4700::1111]:53", syscall.AF_INET6},
	} {
		familyRoutes := darwinDefaultRoutes(candidate.af, candidate.family)
		localIP := darwinRouteLocalIP(candidate.network, candidate.endpoint)
		interfaceName := interfaceForIP(localIP)
		selectedFound := false
		for index := range familyRoutes {
			if interfaceName != "" && familyRoutes[index].Interface == interfaceName {
				familyRoutes[index].LocalIP = localIP
				familyRoutes[index].Selected = true
				selectedFound = true
			}
		}
		if !selectedFound && interfaceName != "" {
			familyRoutes = append(familyRoutes, DefaultRoute{Family: candidate.family, Interface: interfaceName, LocalIP: localIP, Selected: true})
		}
		routes = append(routes, familyRoutes...)
	}
	sort.Slice(routes, func(left, right int) bool {
		if routes[left].Family != routes[right].Family {
			return routes[left].Family < routes[right].Family
		}
		if routes[left].Selected != routes[right].Selected {
			return routes[left].Selected
		}
		return routes[left].Interface < routes[right].Interface
	})
	return routes
}

func darwinRouteLocalIP(network, endpoint string) string {
	connection, err := net.Dial(network, endpoint)
	if err != nil {
		return ""
	}
	defer connection.Close()
	return addressIP(connection.LocalAddr())
}

func darwinDefaultGateway(addressFamily int, interfaceName string) string {
	for _, route := range darwinDefaultRoutes(addressFamily, "") {
		if interfaceName == "" || route.Interface == interfaceName {
			return route.Gateway
		}
	}
	return ""
}

func darwinDefaultRoutes(addressFamily int, family string) []DefaultRoute {
	raw, err := route.FetchRIB(addressFamily, route.RIBTypeRoute, 0)
	if err != nil {
		return nil
	}
	messages, err := route.ParseRIB(route.RIBTypeRoute, raw)
	if err != nil {
		return nil
	}
	result := make([]DefaultRoute, 0, 4)
	seen := make(map[string]struct{})
	for _, message := range messages {
		item, ok := message.(*route.RouteMessage)
		if !ok || item.Flags&syscall.RTF_UP == 0 {
			continue
		}
		if len(item.Addrs) <= syscall.RTAX_DST || !darwinRouteAddressIsDefault(item.Addrs[syscall.RTAX_DST]) {
			continue
		}
		networkInterface, lookupErr := net.InterfaceByIndex(item.Index)
		if lookupErr != nil || networkInterface.Name == "" {
			continue
		}
		gateway := ""
		if len(item.Addrs) > syscall.RTAX_GATEWAY {
			if value := darwinRouteAddressIP(item.Addrs[syscall.RTAX_GATEWAY]); value != nil && !value.IsUnspecified() {
				gateway = value.String()
			}
		}
		key := networkInterface.Name + "|" + gateway
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, DefaultRoute{Family: family, Interface: networkInterface.Name, Gateway: gateway})
	}
	return result
}

func darwinRouteAddressIsDefault(address route.Addr) bool {
	ip := darwinRouteAddressIP(address)
	return ip != nil && ip.IsUnspecified()
}

func darwinRouteAddressIP(address route.Addr) net.IP {
	switch value := address.(type) {
	case *route.Inet4Addr:
		return net.IPv4(value.IP[0], value.IP[1], value.IP[2], value.IP[3])
	case *route.Inet6Addr:
		return net.IP(value.IP[:])
	default:
		return nil
	}
}
