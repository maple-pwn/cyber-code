//go:build !linux && !windows

package platform

import "runtime"

func nativeNotificationTarget() string { return runtime.GOOS }
