//go:build linux

package main

import "strconv"

func platformTracerouteCommand(host string, maxHops int) (string, []string, string, error) {
	if command, err := tracerouteExecutable("tracepath"); err == nil {
		args := []string{"-n", "-m", strconv.Itoa(maxHops), host}
		if stdbuf, stdbufErr := tracerouteExecutable("stdbuf"); stdbufErr == nil {
			return stdbuf, append([]string{"-oL", command}, args...), "tracepath", nil
		}
		return command, args, "tracepath", nil
	}
	command, err := tracerouteExecutable("traceroute")
	if err != nil {
		return "", nil, "", err
	}
	args := []string{"-n", "-m", strconv.Itoa(maxHops), "-w", "1", "-q", "1", host}
	if stdbuf, stdbufErr := tracerouteExecutable("stdbuf"); stdbufErr == nil {
		return stdbuf, append([]string{"-oL", command}, args...), "traceroute", nil
	}
	return command, args, "traceroute", nil
}
