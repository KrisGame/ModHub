//go:build !windows

package runtime

// discoverRegistryCandidates is a no-op on non-Windows platforms.
func discoverRegistryCandidates(_ func(raw, source string)) {}
