//go:build linux

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	gnomeProxySchema      = "org.gnome.system.proxy"
	gnomeHTTPProxySchema  = "org.gnome.system.proxy.http"
	gnomeHTTPSProxySchema = "org.gnome.system.proxy.https"
	gnomeSOCKSProxySchema = "org.gnome.system.proxy.socks"
)

var gvariantListItemPattern = regexp.MustCompile(`'((?:\\.|[^'])*)'`)

type gsettingsReader func(schema, key string) (string, error)

func platformProxyForRequest(request *http.Request) (*url.URL, error) {
	return linuxSystemProxyForRequest(request, readGSettings)
}

func linuxSystemProxyForRequest(request *http.Request, read gsettingsReader) (*url.URL, error) {
	modeValue, err := read(gnomeProxySchema, "mode")
	if err != nil {
		return nil, nil
	}
	switch parseGVariantString(modeValue) {
	case "none", "":
		return nil, nil
	case "auto":
		return nil, fmt.Errorf("GNOME automatic proxy configuration (PAC) is not supported; use manual proxy mode")
	case "manual":
		// Continue below.
	default:
		return nil, nil
	}

	if ignoredValue, readErr := read(gnomeProxySchema, "ignore-hosts"); readErr == nil {
		if proxyBypassHost(request.URL.Hostname(), parseGVariantList(ignoredValue)) {
			return nil, nil
		}
	}

	proxyScheme := "http"
	settingsSchema := gnomeHTTPProxySchema
	useSameProxy := false
	if value, readErr := read(gnomeProxySchema, "use-same-proxy"); readErr == nil {
		useSameProxy = strings.TrimSpace(value) == "true"
	}
	if !useSameProxy && request.URL.Scheme == "https" {
		settingsSchema = gnomeHTTPSProxySchema
	}

	host, port := readGnomeProxyAddress(read, settingsSchema)
	if host == "" || port == 0 {
		host, port = readGnomeProxyAddress(read, gnomeSOCKSProxySchema)
		proxyScheme = "socks5"
	}
	if host == "" || port == 0 {
		return nil, nil
	}

	proxyURL, err := validateProxyURL(proxyScheme + "://" + net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	if proxyScheme == "http" {
		applyGnomeProxyAuthentication(proxyURL, read)
	}
	return proxyURL, nil
}

func readGSettings(schema, key string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "gsettings", "get", schema, key).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func readGnomeProxyAddress(read gsettingsReader, schema string) (string, int) {
	hostValue, err := read(schema, "host")
	if err != nil {
		return "", 0
	}
	portValue, err := read(schema, "port")
	if err != nil {
		return "", 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(portValue))
	if err != nil || port < 1 || port > 65535 {
		return "", 0
	}
	return parseGVariantString(hostValue), port
}

func applyGnomeProxyAuthentication(proxyURL *url.URL, read gsettingsReader) {
	enabledValue, err := read(gnomeHTTPProxySchema, "use-authentication")
	if err != nil || strings.TrimSpace(enabledValue) != "true" {
		return
	}
	usernameValue, usernameErr := read(gnomeHTTPProxySchema, "authentication-user")
	passwordValue, passwordErr := read(gnomeHTTPProxySchema, "authentication-password")
	if usernameErr != nil || passwordErr != nil {
		return
	}
	proxyURL.User = url.UserPassword(parseGVariantString(usernameValue), parseGVariantString(passwordValue))
}

func parseGVariantString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		value = value[1 : len(value)-1]
	}
	value = strings.ReplaceAll(value, `\'`, `'`)
	return strings.ReplaceAll(value, `\\`, `\`)
}

func parseGVariantList(value string) []string {
	matches := gvariantListItemPattern.FindAllStringSubmatch(value, -1)
	items := make([]string, 0, len(matches))
	for _, match := range matches {
		items = append(items, parseGVariantString("'"+match[1]+"'"))
	}
	return items
}
