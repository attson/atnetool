//go:build linux

package main

import (
	"strings"
	"testing"
)

func TestParseLinuxDefaultRoute(t *testing.T) {
	contents := strings.NewReader("Iface\tDestination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\neth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n")
	route, err := parseLinuxIPv4DefaultRoute(contents)
	if err != nil {
		t.Fatal(err)
	}
	if route.Interface != "eth0" || route.Gateway != "192.168.1.1" || route.Metric != 100 {
		t.Fatalf("route = %+v", route)
	}
}

func TestParseLinuxDefaultRoutesKeepsCompetingRoutesByMetric(t *testing.T) {
	contents := strings.NewReader("Iface\tDestination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n" +
		"tun0\t00000000\t00000000\t0001\t0\t0\t5\t00000000\t0\t0\t0\n" +
		"eth0\t00000000\t0101A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n")
	routes, err := parseLinuxIPv4DefaultRoutes(contents)
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 2 || routes[0].Interface != "tun0" || routes[1].Interface != "eth0" {
		t.Fatalf("routes = %+v", routes)
	}
}
