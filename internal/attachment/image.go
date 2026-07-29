package attachment

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"cyber-code/internal/core"
)

const MaxImageBytes int64 = 10 << 20

var (
	ErrImageOutsideWorkspace = errors.New("image is outside the workspace")
	ErrImageTooLarge         = errors.New("image is too large")
	ErrUnsupportedImage      = errors.New("unsupported image format")
)

func LoadImage(workspace, path string) (core.ContentBlock, error) {
	root, err := canonicalDirectory(workspace)
	if err != nil {
		return core.ContentBlock{}, fmt.Errorf("resolve image workspace: %w", err)
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return core.ContentBlock{}, fmt.Errorf("resolve image path: %w", err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil || !withinRoot(root, resolved) {
		return core.ContentBlock{}, ErrImageOutsideWorkspace
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return core.ContentBlock{}, fmt.Errorf("inspect image: %w", err)
	}
	if !info.Mode().IsRegular() {
		return core.ContentBlock{}, fmt.Errorf("image is not a regular file")
	}
	if info.Size() > MaxImageBytes {
		return core.ContentBlock{}, ErrImageTooLarge
	}
	file, err := os.Open(filepath.Clean(resolved))
	if err != nil {
		return core.ContentBlock{}, fmt.Errorf("open image: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil {
		return core.ContentBlock{}, fmt.Errorf("read image: %w", err)
	}
	if int64(len(data)) > MaxImageBytes {
		return core.ContentBlock{}, ErrImageTooLarge
	}
	mediaType := detectImageType(data)
	if mediaType == "" {
		return core.ContentBlock{}, ErrUnsupportedImage
	}
	return core.ContentBlock{Type: core.ContentImage, MediaType: mediaType, Data: base64.StdEncoding.EncodeToString(data)}, nil
}

func canonicalDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func withinRoot(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)))
}

func detectImageType(data []byte) string {
	switch {
	case len(data) >= 8 && bytes.Equal(data[:8], []byte("\x89PNG\r\n\x1a\n")):
		return "image/png"
	case len(data) >= 3 && bytes.Equal(data[:3], []byte{0xff, 0xd8, 0xff}):
		return "image/jpeg"
	case len(data) >= 6 && (bytes.Equal(data[:6], []byte("GIF87a")) || bytes.Equal(data[:6], []byte("GIF89a"))):
		return "image/gif"
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp"
	default:
		return ""
	}
}
