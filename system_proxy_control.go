package main

// SystemProxySnapshot stores only the operating-system values ATNetool changes.
// It is persisted before activation so a later launch can recover from an
// unclean exit without guessing the user's previous proxy configuration.
type SystemProxySnapshot struct {
	Platform string            `json:"platform"`
	Values   map[string]string `json:"values,omitempty"`
	Integers map[string]uint64 `json:"integers,omitempty"`
	Present  map[string]bool   `json:"present,omitempty"`
}
