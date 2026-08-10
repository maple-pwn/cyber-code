package runtimeapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestOIDCJWKResolverDiscoversCachesAndRefreshesUnknownKey(t *testing.T) {
	publicA, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicB, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	keys := map[string]ed25519.PublicKey{"key-a": publicA}
	discoveryRequests, jwksRequests := 0, 0
	server := newOIDCTestServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			discoveryRequests++
			_ = json.NewEncoder(writer).Encode(map[string]string{"issuer": "http://" + request.Host, "jwks_uri": "http://" + request.Host + "/jwks"})
		case "/jwks":
			jwksRequests++
			payload := make([]map[string]string, 0, len(keys))
			for id, key := range keys {
				payload = append(payload, map[string]string{"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "kid": id, "x": base64.RawURLEncoding.EncodeToString(key)})
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"keys": payload})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	resolver, err := NewOIDCJWKResolver(OIDCJWKResolverOptions{Issuer: server.URL, HTTPClient: server.Client(), CacheTTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "key-a", "EdDSA"); err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "key-a", "EdDSA"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	keys["key-b"] = publicB
	mu.Unlock()
	if _, err := resolver.Resolve(context.Background(), "key-b", "EdDSA"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if discoveryRequests != 2 || jwksRequests != 2 {
		t.Fatalf("requests discovery=%d jwks=%d, want one initial fetch and one unknown-kid refresh", discoveryRequests, jwksRequests)
	}
}

func TestOIDCJWKResolverBoundsStaleKeys(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	failing := false
	server := newOIDCTestServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if failing {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if request.URL.Path == "/.well-known/openid-configuration" {
			_ = json.NewEncoder(writer).Encode(map[string]string{"issuer": "http://" + request.Host, "jwks_uri": "http://" + request.Host + "/jwks"})
			return
		}
		_ = json.NewEncoder(writer).Encode(map[string]any{"keys": []map[string]string{{"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "kid": "key-a", "x": base64.RawURLEncoding.EncodeToString(publicKey)}}})
	}))
	defer server.Close()
	resolver, err := NewOIDCJWKResolver(OIDCJWKResolverOptions{Issuer: server.URL, HTTPClient: server.Client(), CacheTTL: time.Minute, StaleGrace: 2 * time.Minute, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "key-a", "EdDSA"); err != nil {
		t.Fatal(err)
	}
	failing = true
	now = now.Add(90 * time.Second)
	if _, err := resolver.Resolve(context.Background(), "key-a", "EdDSA"); err != nil {
		t.Fatalf("key inside stale grace was rejected: %v", err)
	}
	now = now.Add(2 * time.Minute)
	if _, err := resolver.Resolve(context.Background(), "key-a", "EdDSA"); err == nil {
		t.Fatal("key beyond stale grace was accepted")
	}
}

func TestOIDCJWKResolverRejectsDiscoveryIssuerMismatch(t *testing.T) {
	server := newOIDCTestServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_ = json.NewEncoder(writer).Encode(map[string]string{"issuer": "https://attacker.example", "jwks_uri": "http://" + request.Host + "/jwks"})
	}))
	defer server.Close()
	resolver, err := NewOIDCJWKResolver(OIDCJWKResolverOptions{Issuer: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Resolve(context.Background(), "key-a", "EdDSA"); err == nil {
		t.Fatal("discovery issuer mismatch was accepted")
	}
}

func TestOIDCJWKResolverParsesRS256AndRejectsInsecureRemoteIssuer(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	modulus := base64.RawURLEncoding.EncodeToString(privateKey.PublicKey.N.Bytes())
	exponent := base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1})
	parsed, err := parseOIDCJWK("RSA", "", "RS256", "", modulus, exponent)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok || publicKey.E != 65537 || publicKey.N.Cmp(privateKey.PublicKey.N) != 0 {
		t.Fatalf("parsed RSA key = %#v", parsed)
	}
	if _, err := NewOIDCJWKResolver(OIDCJWKResolverOptions{Issuer: "http://identity.example.test"}); err == nil {
		t.Fatal("non-loopback plaintext issuer was accepted")
	}
	if _, err := NewOIDCJWKResolver(OIDCJWKResolverOptions{Issuer: "https://identity.example.test?tenant=a"}); err == nil {
		t.Fatal("issuer with query parameters was accepted")
	}
	if _, err := NewOIDCJWKResolver(OIDCJWKResolverOptions{Issuer: "https://identity.example.test", CacheTTL: 25 * time.Hour}); err == nil {
		t.Fatal("unbounded cache TTL was accepted")
	}
}

func TestDiscoveredOIDCAuthenticatorUsesRotatingResolver(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]string{"issuer": issuer, "jwks_uri": issuer + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(writer).Encode(map[string]any{"keys": []map[string]string{{"kty": "OKP", "crv": "Ed25519", "alg": "EdDSA", "kid": "test-key", "x": base64.RawURLEncoding.EncodeToString(publicKey)}}})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	issuer = server.URL
	clock := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	authenticator, err := NewDiscoveredOIDCRemoteAuthenticator("cyber-code", OIDCJWKResolverOptions{Issuer: issuer, HTTPClient: server.Client(), Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	authenticator.clock = func() time.Time { return clock }
	token := signOIDCTestToken(t, privateKey, map[string]any{"iss": issuer, "aud": "cyber-code", "sub": "owner@example.test", "exp": clock.Add(time.Hour).Unix(), "tenant_id": "tenant-a", "sid": "session-a", "role": "owner", "task_context_id": "context-a", "controller_id": "controller-a", "capabilities": []string{"events"}})
	if _, err := authenticator.Authenticate(context.Background(), token); err != nil {
		t.Fatal(err)
	}
}

func newOIDCTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}
