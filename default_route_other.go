//go:build !linux && !windows && !darwin

package main

import "net"

func detectDefaultRoutes() []DefaultRoute {
	routes := make([]DefaultRoute, 0, 2)
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
		routes = append(routes, DefaultRoute{Family: candidate.family, Interface: interfaceForIP(localIP), LocalIP: localIP, Selected: true})
	}
	return routes
}
