package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"unicode"

	"cyber-code/internal/product"
	updatepkg "cyber-code/internal/update"
)

const (
	signingKeyEnvironment = "CYBER_CODE_UPDATE_SIGNING_KEY"
	maxSigningKeyBytes    = 4096
)

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }
func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "release-manifest:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	if len(args) == 0 {
		return fmt.Errorf("a validate, manifest, or public-key subcommand is required")
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "manifest":
		return runManifest(args[1:], stdout, stderr, getenv)
	case "public-key":
		return runPublicKey(args[1:], stdout, stderr, getenv)
	default:
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func runValidate(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("validate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var version, baseURL string
	flags.StringVar(&version, "version", "", "semantic release version")
	flags.StringVar(&baseURL, "base-url", "", "HTTPS artifact base URL")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("validate does not accept positional arguments")
	}
	if !updatepkg.ValidReleaseVersion(version) {
		return fmt.Errorf("--version must use canonical semantic version syntax")
	}
	parsed, err := parseArtifactBaseURL(baseURL)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, parsed.String())
	return err
}

func runManifest(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("manifest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var version, publishedAt, baseURL, keyFile string
	var artifactSpecs stringList
	flags.StringVar(&version, "version", "", "semantic release version")
	flags.StringVar(&publishedAt, "published-at", "", "canonical RFC3339 UTC publication time")
	flags.StringVar(&baseURL, "base-url", "", "HTTPS artifact base URL")
	flags.StringVar(&keyFile, "key-file", "", "file containing a base64 Ed25519 seed or private key")
	flags.Var(&artifactSpecs, "artifact", "artifact mapping goos/goarch=path (repeatable)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("manifest does not accept positional arguments")
	}
	if strings.TrimSpace(version) == "" || strings.TrimSpace(publishedAt) == "" || strings.TrimSpace(baseURL) == "" {
		return fmt.Errorf("--version, --published-at, and --base-url are required")
	}
	if len(artifactSpecs) == 0 {
		return fmt.Errorf("at least one --artifact is required")
	}
	parsedBaseURL, err := parseArtifactBaseURL(baseURL)
	if err != nil {
		return err
	}
	artifacts := make([]updatepkg.Artifact, 0, len(artifactSpecs))
	for _, spec := range artifactSpecs {
		artifact, err := buildArtifact(spec, parsedBaseURL)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifact)
	}
	privateKey, err := loadSigningKey(keyFile, getenv)
	if err != nil {
		return err
	}
	document, err := updatepkg.BuildEnvelope(updatepkg.Payload{
		SchemaVersion: updatepkg.ManifestSchemaVersion,
		Product:       product.Name,
		Version:       version,
		PublishedAt:   publishedAt,
		Artifacts:     artifacts,
	}, privateKey)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(document))
	return err
}

func runPublicKey(args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	flags := flag.NewFlagSet("public-key", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var keyFile string
	flags.StringVar(&keyFile, "key-file", "", "file containing a base64 Ed25519 seed or private key")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("public-key does not accept positional arguments")
	}
	privateKey, err := loadSigningKey(keyFile, getenv)
	if err != nil {
		return err
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	_, err = fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(publicKey))
	return err
}

func parseArtifactBaseURL(value string) (*url.URL, error) {
	if strings.TrimSpace(value) != value {
		return nil, fmt.Errorf("--base-url must not contain surrounding whitespace")
	}
	if len(value) == 0 || len(value) > 2048 || strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character) || strings.ContainsRune("'\"\\", character)
	}) >= 0 {
		return nil, fmt.Errorf("--base-url contains characters that are unsafe for release build injection")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("--base-url must be an absolute HTTPS URL without user information, query, or fragment")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed, nil
}

func buildArtifact(spec string, baseURL *url.URL) (updatepkg.Artifact, error) {
	platform, localPath, found := strings.Cut(spec, "=")
	if !found || strings.TrimSpace(localPath) == "" {
		return updatepkg.Artifact{}, fmt.Errorf("--artifact must use goos/goarch=path")
	}
	goos, goarch, found := strings.Cut(platform, "/")
	if !found || goos == "" || goarch == "" || strings.Contains(goarch, "/") {
		return updatepkg.Artifact{}, fmt.Errorf("--artifact platform must use goos/goarch")
	}
	file, err := os.Open(localPath)
	if err != nil {
		return updatepkg.Artifact{}, fmt.Errorf("open artifact for %s/%s: %w", goos, goarch, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return updatepkg.Artifact{}, fmt.Errorf("inspect artifact for %s/%s: %w", goos, goarch, err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return updatepkg.Artifact{}, fmt.Errorf("artifact for %s/%s must be a non-empty regular file", goos, goarch)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return updatepkg.Artifact{}, fmt.Errorf("hash artifact for %s/%s: %w", goos, goarch, err)
	}
	artifactURL := *baseURL
	artifactURL.Path = pathpkg.Join(artifactURL.Path, filepath.Base(localPath))
	artifactURL.RawPath = ""
	return updatepkg.Artifact{
		GOOS: goos, GOARCH: goarch, URL: artifactURL.String(),
		SHA256: fmt.Sprintf("%x", hash.Sum(nil)), Size: info.Size(),
	}, nil
}

func loadSigningKey(keyFile string, getenv func(string) string) (ed25519.PrivateKey, error) {
	var encoded string
	if strings.TrimSpace(keyFile) != "" {
		file, err := os.Open(keyFile)
		if err != nil {
			return nil, fmt.Errorf("open signing key file: %w", err)
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxSigningKeyBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read signing key file: %w", readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close signing key file: %w", closeErr)
		}
		if len(data) > maxSigningKeyBytes {
			return nil, fmt.Errorf("signing key file exceeds %d bytes", maxSigningKeyBytes)
		}
		encoded = strings.TrimSpace(string(data))
	} else if getenv != nil {
		encoded = strings.TrimSpace(getenv(signingKeyEnvironment))
	}
	if encoded == "" {
		return nil, fmt.Errorf("provide --key-file or %s", signingKeyEnvironment)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("signing key must be valid base64")
	}
	switch len(raw) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(raw), nil
	case ed25519.PrivateKeySize:
		derived := ed25519.NewKeyFromSeed(raw[:ed25519.SeedSize])
		if !bytes.Equal(raw, derived) {
			return nil, fmt.Errorf("signing key is not a consistent Ed25519 private key")
		}
		return ed25519.PrivateKey(append([]byte(nil), raw...)), nil
	default:
		return nil, fmt.Errorf("signing key must decode to an Ed25519 seed or private key")
	}
}
