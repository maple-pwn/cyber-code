//go:build linux

package platform

// Bubblewrap sandbox construction lives in exec_unix.go so the same process
// group lifecycle applies to both sandboxed and policy-only execution.
