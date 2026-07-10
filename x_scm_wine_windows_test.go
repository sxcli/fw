//go:build windows

package sxclifw_test

// wineCleanup is a no-op on windows: the SCM path runs natively there,
// no wine involved.
func wineCleanup() {}
