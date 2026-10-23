//go:build windows

package main

import "strconv"

func platformTracerouteCommand(host string, maxHops int) (string, []string, string, error) {
	command, err := tracerouteExecutable("tracert")
	if err != nil {
		return "", nil, "", err
	}
	return command, []string{"-d", "-h", strconv.Itoa(maxHops), "-w", "700", host}, "tracert", nil
}
