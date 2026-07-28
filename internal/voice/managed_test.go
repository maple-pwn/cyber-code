package voice

import (
	"context"
	"testing"
)

func TestVoiceManagedServiceCapturesAndTranscribes(t *testing.T) {
	service := NewManagedService(captureStub{audio: []byte("audio")}, &MockTranscriber{Response: "hello"})
	service.Enable()
	text, err := service.CaptureAndTranscribe(context.Background())
	if err != nil || text != "hello" {
		t.Fatalf("text = %q, error = %v", text, err)
	}
	if service.IsRecording() {
		t.Fatal("one-shot managed service reported active recording")
	}
	if err := service.StartRecording(context.Background()); err == nil {
		t.Fatal("legacy start unexpectedly succeeded for managed service")
	}
}

type captureStub struct{ audio []byte }

func (stub captureStub) Capture(context.Context) ([]byte, error) { return stub.audio, nil }
