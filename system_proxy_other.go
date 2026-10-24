//go:build !windows && !linux && !darwin

package main

import (
	"net/http"
	"net/url"
)

func platformProxyForRequest(*http.Request) (*url.URL, error) {
	return nil, nil
}
