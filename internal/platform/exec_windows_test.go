//go:build windows

package platform

func expectedBestEffortIsolation() Isolation { return IsolationJobObject }
