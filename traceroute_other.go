//go:build !windows && !linux

package main

import "strconv"

func platformTracerouteCommand(host string, maxHops int) (string, []string, string, error) {
	command, err := tracerouteExecutable("traceroute")
	if err != nil {
		return "", nil, "", err
	}
	return command, []string{"-n", "-m", strconv.Itoa(maxHops), "-w", "1", "-q", "1", host}, "traceroute", nil
}
