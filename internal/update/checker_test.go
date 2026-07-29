package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestCheckRequiresExplicitOptInWithoutNetworkAccess(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}
	result, err := Check(context.Background(), Options{Client: client})
	if err != nil || result.Checked || called {
		t.Fatalf("result=%#v called=%t error=%v", result, called, err)
	}
}

func TestCheckRequiresHTTPSAndValidSignedMetadata(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	body, err := BuildEnvelope(Payload{
		SchemaVersion: ManifestSchemaVersion, Product: "cyber-code", Version: "2.2.0", PublishedAt: "2026-07-29T00:00:00Z",
		Artifacts: []Artifact{{GOOS: "linux", GOARCH: "amd64", URL: "https://example.test/cyber-code", SHA256: strings.Repeat("a", 64), Size: 123}},
	}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" {
			t.Fatalf("request URL = %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})}
	result, err := Check(context.Background(), Options{
		Enabled: true, CurrentVersion: "2.1.88", MetadataURL: "https://updates.example.test/latest.json",
		PublicKey: publicKey, Client: client, Timeout: time.Second, GOOS: "linux", GOARCH: "amd64",
	})
	if err != nil || !result.Checked || !result.UpdateAvailable || result.LatestVersion != "2.2.0" || result.DownloadURL == "" || result.ArtifactSHA256 != strings.Repeat("a", 64) || result.ArtifactSize != 123 {
		t.Fatalf("result=%#v error=%v", result, err)
	}
	if _, err := Check(context.Background(), Options{Enabled: true, CurrentVersion: "2.1.88", MetadataURL: "http://updates.example.test/latest.json", PublicKey: publicKey, Client: client}); err == nil {
		t.Fatal("insecure metadata URL was accepted")
	}
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)/2] ^= 1
	badClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(tampered)))}, nil
	})}
	if _, err := Check(context.Background(), Options{Enabled: true, CurrentVersion: "2.1.88", MetadataURL: "https://updates.example.test/latest.json", PublicKey: publicKey, Client: badClient, GOOS: "linux", GOARCH: "amd64"}); err == nil {
		t.Fatal("tampered metadata envelope was accepted")
	}
}

func TestCheckRejectsInsecureFinalResponseURL(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		insecure, err := http.NewRequest(http.MethodGet, "http://redirected.example.test/latest.json", nil)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("{}")), Request: insecure}, nil
	})}
	if _, err := Check(context.Background(), Options{
		Enabled: true, CurrentVersion: "1.0.0", MetadataURL: "https://updates.example.test/latest.json",
		PublicKey: publicKey, Client: client,
	}); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("insecure redirect error = %v", err)
	}
}

func TestCheckBoundsRequestTimeout(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	started := time.Now()
	_, err = Check(context.Background(), Options{
		Enabled: true, CurrentVersion: "1.0.0", MetadataURL: "https://updates.example.test/latest.json",
		PublicKey: privateKey.Public().(ed25519.PublicKey), Client: client, Timeout: 20 * time.Millisecond, GOOS: "linux", GOARCH: "amd64",
	})
	if err == nil || time.Since(started) > time.Second {
		t.Fatalf("bounded timeout error=%v elapsed=%s", err, time.Since(started))
	}
}

func TestNewerVersionUsesSemanticOrdering(t *testing.T) {
	for _, test := range []struct {
		current, latest string
		want            bool
	}{
		{current: "2.9.0", latest: "2.10.0", want: true},
		{current: "v2.10.0", latest: "2.10.0", want: false},
		{current: "2.10.0", latest: "2.10.0-beta.1", want: false},
	} {
		got, err := NewerVersion(test.current, test.latest)
		if err != nil || got != test.want {
			t.Fatalf("NewerVersion(%q, %q) = %t, %v", test.current, test.latest, got, err)
		}
	}
}
