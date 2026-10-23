//go:build linux

package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
)

func detectDefaultRoutes() []DefaultRoute {
	routes := make([]DefaultRoute, 0, 4)
	if file, err := os.Open("/proc/net/route"); err == nil {
		if detected, parseErr := parseLinuxIPv4DefaultRoutes(file); parseErr == nil {
			routes = append(routes, detected...)
		}
		_ = file.Close()
	}
	ipv4Local := routeLocalIP("udp4", "1.1.1.1:53")
	if len(routes) == 0 && ipv4Local != "" {
		routes = append(routes, DefaultRoute{Family: "IPv4", Interface: interfaceForIP(ipv4Local), LocalIP: ipv4Local})
	}
	if localIP := routeLocalIP("udp6", "[2606:4700:4700::1111]:53"); localIP != "" {
		routes = append(routes, DefaultRoute{Family: "IPv6", Interface: interfaceForIP(localIP), LocalIP: localIP, Selected: true})
	}
	if ipv4Local != "" {
		activeInterface := interfaceForIP(ipv4Local)
		for index := range routes {
			if routes[index].Family == "IPv4" && routes[index].Interface == activeInterface {
				routes[index].LocalIP = ipv4Local
				routes[index].Selected = true
				break
			}
		}
	}
	return routes
}

func parseLinuxIPv4DefaultRoute(reader io.Reader) (DefaultRoute, error) {
	routes, err := parseLinuxIPv4DefaultRoutes(reader)
	if err != nil {
		return DefaultRoute{}, err
	}
	return routes[0], nil
}

func parseLinuxIPv4DefaultRoutes(reader io.Reader) ([]DefaultRoute, error) {
	scanner := bufio.NewScanner(reader)
	routes := make([]DefaultRoute, 0, 4)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 || fields[1] != "00000000" || fields[7] != "00000000" {
			continue
		}
		flags, _ := strconv.ParseUint(fields[3], 16, 32)
		if flags&0x1 == 0 {
			continue
		}
		metric, err := strconv.Atoi(fields[6])
		if err != nil {
			continue
		}
		gateway, err := decodeLinuxIPv4(fields[2])
		if err != nil {
			continue
		}
		routes = append(routes, DefaultRoute{Family: "IPv4", Interface: fields[0], Gateway: gateway, Metric: metric})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("default route not found")
	}
	sort.Slice(routes, func(left, right int) bool {
		if routes[left].Metric != routes[right].Metric {
			return routes[left].Metric < routes[right].Metric
		}
		return routes[left].Interface < routes[right].Interface
	})
	return routes, nil
}

func decodeLinuxIPv4(value string) (string, error) {
	bytes, err := hex.DecodeString(value)
	if err != nil || len(bytes) != 4 {
		return "", fmt.Errorf("invalid route address %q", value)
	}
	return net.IPv4(bytes[3], bytes[2], bytes[1], bytes[0]).String(), nil
}

func routeLocalIP(network, endpoint string) string {
	connection, err := net.Dial(network, endpoint)
	if err != nil {
		return ""
	}
	defer connection.Close()
	return addressIP(connection.LocalAddr())
}
