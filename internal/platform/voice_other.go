//go:build !linux && !windows

package platform

import "runtime"

func nativeVoiceTarget() string { return runtime.GOOS }
