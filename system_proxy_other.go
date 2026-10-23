//go:build !windows && !linux

package main

import (
	"net/http"
	"net/url"
)

func platformProxyForRequest(*http.Request) (*url.URL, error) {
	return nil, nil
}
