package platform

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"
)

type VoiceOptions struct {
	GOOS        string
	LookPath    CommandFinder
	RunCommand  CommandRunner
	Authorizer  Authorizer
	Workspace   string
	TempDir     string
	SampleRate  int
	Channels    int
	MaxDuration time.Duration
	MaxBytes    int64
}

type VoiceRecorder struct {
	capability Capability
	program    string
	backend    string
	options    VoiceOptions
}

func NewVoiceRecorder(options VoiceOptions) *VoiceRecorder {
	if options.GOOS == "" {
		options.GOOS = nativeVoiceTarget()
	}
	if options.LookPath == nil {
		options.LookPath = exec.LookPath
	}
	if options.Workspace == "" {
		options.Workspace, _ = os.Getwd()
	}
	if options.TempDir == "" {
		options.TempDir = os.TempDir()
	}
	if options.SampleRate <= 0 {
		options.SampleRate = 16000
	}
	if options.Channels <= 0 {
		options.Channels = 1
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = time.Minute
	}
	if options.MaxBytes <= 0 {
		options.MaxBytes = 16 << 20
	}
	if options.RunCommand == nil {
		options.RunCommand = managedCommandRunner(options.Workspace)
	}
	backend, program, reason := detectVoiceBackend(options.GOOS, options.LookPath)
	return &VoiceRecorder{
		capability: Capability{Available: program != "", Backend: backend, Reason: reason},
		program:    program, backend: backend, options: options,
	}
}

func (recorder *VoiceRecorder) Capability() Capability { return recorder.capability }

func (recorder *VoiceRecorder) Capture(ctx context.Context) ([]byte, error) {
	if !recorder.capability.Available {
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, recorder.capability.Reason)
	}
	if err := authorizeMedia(ctx, recorder.options.Authorizer, recorder.options.Workspace, "voice_recording", recorder.program); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(recorder.options.TempDir, "claude-go-voice-*.raw")
	if err != nil {
		return nil, fmt.Errorf("create voice capture file: %w", err)
	}
	path := file.Name()
	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(path)
		return nil, fmt.Errorf("close voice capture file: %w", closeErr)
	}
	defer os.Remove(path)
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, fmt.Errorf("restrict voice capture file: %w", err)
	}

	duration := recorder.captureDuration()
	captureCtx, cancel := context.WithTimeout(ctx, duration+2*time.Second)
	defer cancel()
	arguments := voiceArguments(recorder.options.GOOS, recorder.backend, recorder.options, duration, path)
	if err := recorder.options.RunCommand(captureCtx, recorder.program, arguments); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect voice capture: %w", err)
	}
	if info.Size() > recorder.options.MaxBytes {
		return nil, fmt.Errorf("voice capture exceeds %d bytes", recorder.options.MaxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read voice capture: %w", err)
	}
	return data, nil
}

func (recorder *VoiceRecorder) captureDuration() time.Duration {
	bytesPerSecond := int64(recorder.options.SampleRate * recorder.options.Channels * 2)
	if bytesPerSecond <= 0 {
		return recorder.options.MaxDuration
	}
	sizeDuration := time.Duration(float64(recorder.options.MaxBytes) / float64(bytesPerSecond) * float64(time.Second))
	if sizeDuration <= 0 {
		return time.Millisecond
	}
	return min(recorder.options.MaxDuration, sizeDuration)
}

func detectVoiceBackend(goos string, lookPath CommandFinder) (string, string, string) {
	type candidate struct{ command, backend string }
	var candidates []candidate
	switch goos {
	case "linux":
		candidates = []candidate{{"rec", "sox"}, {"arecord", "alsa"}, {"ffmpeg", "ffmpeg-alsa"}}
	case "windows":
		candidates = []candidate{{"ffmpeg.exe", "ffmpeg-dshow"}, {"ffmpeg", "ffmpeg-dshow"}}
	default:
		return "", "", "voice capture is unsupported on " + goos
	}
	for _, candidate := range candidates {
		if path, err := lookPath(candidate.command); err == nil {
			return candidate.backend, path, ""
		}
	}
	return "", "", "no supported audio recording command was found"
}

func voiceArguments(goos, backend string, options VoiceOptions, duration time.Duration, output string) []string {
	rate, channels := strconv.Itoa(options.SampleRate), strconv.Itoa(options.Channels)
	seconds := strconv.FormatFloat(duration.Seconds(), 'f', 3, 64)
	switch backend {
	case "sox":
		return []string{"--clobber", "-q", "-r", rate, "-c", channels, "-b", "16", "-e", "signed-integer", output, "trim", "0", seconds}
	case "alsa":
		return []string{"-q", "-f", "S16_LE", "-r", rate, "-c", channels, "-t", "raw", "-d", strconv.Itoa(max(1, int(math.Ceil(duration.Seconds())))), output}
	case "ffmpeg-alsa":
		return []string{"-nostdin", "-y", "-f", "alsa", "-i", "default", "-t", seconds, "-f", "s16le", "-ar", rate, "-ac", channels, output}
	default:
		_ = goos
		return []string{"-nostdin", "-y", "-f", "dshow", "-i", "audio=default", "-t", seconds, "-f", "s16le", "-ar", rate, "-ac", channels, output}
	}
}
