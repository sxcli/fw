//go:build !unix

// Package syslog is unavailable on this platform: importing it is
// harmless and registers nothing. Enabling the "syslog" service in a
// binary built for this platform fails with an unknown service id.
package syslog
