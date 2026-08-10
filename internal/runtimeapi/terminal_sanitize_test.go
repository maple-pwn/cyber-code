package runtimeapi

import (
	"bytes"
	"testing"
)

func TestTerminalOutputSanitizerBlocksHostileSequencesAcrossChunks(t *testing.T) {
	sanitizer := newTerminalOutputSanitizer()
	parts := [][]byte{
		[]byte("safe\x1b]52;c;secret"),
		[]byte("\x1b\\after\x1bPpayload"),
		[]byte("\x1b\\\xe2\x80"),
		[]byte("\xaegood\x00\x07\x1b[31mred\x1b[6n"),
	}
	var output []byte
	for _, part := range parts {
		output = append(output, sanitizer.Push(part)...)
	}
	output = append(output, sanitizer.Flush()...)
	if got, want := string(output), "safeaftergood\x1b[31mred"; got != want {
		t.Fatalf("sanitized = %q, want %q", got, want)
	}
	if bytes.Contains(output, []byte("secret")) || bytes.Contains(output, []byte("payload")) {
		t.Fatalf("hostile payload survived: %q", output)
	}
}

func TestTerminalOutputSanitizerPreservesBasicTerminalText(t *testing.T) {
	sanitizer := newTerminalOutputSanitizer()
	output := append(sanitizer.Push([]byte("line\r\n\t\x1b[2Kdone")), sanitizer.Flush()...)
	if got, want := string(output), "line\r\n\t\x1b[2Kdone"; got != want {
		t.Fatalf("sanitized = %q, want %q", got, want)
	}
}
