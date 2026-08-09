package runtimeapi

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const maxOIDCMetadataBytes int64 = 1 << 20

type OIDCJWKResolverOptions struct {
	Issuer     string
	HTTPClient *http.Client
	CacheTTL   time.Duration
	StaleGrace time.Duration
	Now        func() time.Time
}

type oidcJWKCacheEntry struct {
	algorithm string
	key       crypto.PublicKey
}

type OIDCJWKResolver struct {
	issuer     string
	httpClient *http.Client
	cacheTTL   time.Duration
	staleGrace time.Duration
	now        func() time.Time

	mu        sync.Mutex
	keys      map[string]oidcJWKCacheEntry
	fetchedAt time.Time
}

func NewOIDCJWKResolver(options OIDCJWKResolverOptions) (*OIDCJWKResolver, error) {
	issuer, err := validateOIDCRemoteURL(options.Issuer)
	if err != nil {
		return nil, errors.New("OIDC issuer must use HTTPS")
	}
	cacheTTL := options.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = 5 * time.Minute
	}
	if cacheTTL > 24*time.Hour {
		return nil, errors.New("OIDC key cache TTL is too long")
	}
	staleGrace := options.StaleGrace
	if staleGrace < 0 {
		return nil, errors.New("OIDC stale-key grace must not be negative")
	}
	if staleGrace == 0 {
		staleGrace = 2 * time.Minute
	}
	if staleGrace > 24*time.Hour {
		return nil, errors.New("OIDC stale-key grace is too long")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &OIDCJWKResolver{issuer: strings.TrimRight(issuer.String(), "/"), httpClient: client, cacheTTL: cacheTTL, staleGrace: staleGrace, now: now, keys: make(map[string]oidcJWKCacheEntry)}, nil
}

func (resolver *OIDCJWKResolver) Resolve(ctx context.Context, keyID, algorithm string) (crypto.PublicKey, error) {
	if resolver == nil || strings.TrimSpace(keyID) == "" || (algorithm != "EdDSA" && algorithm != "RS256") {
		return nil, errors.New("OIDC signing key unavailable")
	}
	resolver.mu.Lock()
	defer resolver.mu.Unlock()
	now := resolver.now()
	entry, found := resolver.keys[keyID]
	if found && entry.algorithm == algorithm && now.Before(resolver.fetchedAt.Add(resolver.cacheTTL)) {
		return entry.key, nil
	}
	refreshErr := resolver.refresh(ctx)
	if refreshErr == nil {
		entry, found = resolver.keys[keyID]
		if found && entry.algorithm == algorithm {
			return entry.key, nil
		}
		return nil, errors.New("OIDC signing key unavailable")
	}
	if found && entry.algorithm == algorithm && !resolver.fetchedAt.IsZero() && !now.After(resolver.fetchedAt.Add(resolver.cacheTTL+resolver.staleGrace)) {
		return entry.key, nil
	}
	return nil, errors.New("OIDC signing key unavailable")
}

func (resolver *OIDCJWKResolver) refresh(ctx context.Context) error {
	discoveryURL := resolver.issuer + "/.well-known/openid-configuration"
	var discovery struct {
		Issuer  string `json:"issuer"`
		JWKsURI string `json:"jwks_uri"`
	}
	if err := resolver.getJSON(ctx, discoveryURL, &discovery); err != nil {
		return err
	}
	if strings.TrimRight(discovery.Issuer, "/") != resolver.issuer {
		return errors.New("OIDC discovery issuer mismatch")
	}
	jwksURL, err := validateOIDCRemoteURL(discovery.JWKsURI)
	if err != nil {
		return errors.New("OIDC JWKS URL is invalid")
	}
	var document struct {
		Keys []struct {
			Type      string `json:"kty"`
			Curve     string `json:"crv"`
			Algorithm string `json:"alg"`
			KeyID     string `json:"kid"`
			X         string `json:"x"`
			N         string `json:"n"`
			E         string `json:"e"`
		} `json:"keys"`
	}
	if err := resolver.getJSON(ctx, jwksURL.String(), &document); err != nil {
		return err
	}
	keys := make(map[string]oidcJWKCacheEntry, len(document.Keys))
	for _, encoded := range document.Keys {
		if encoded.KeyID == "" || len(encoded.KeyID) > 256 {
			continue
		}
		key, err := parseOIDCJWK(encoded.Type, encoded.Curve, encoded.Algorithm, encoded.X, encoded.N, encoded.E)
		if err == nil {
			keys[encoded.KeyID] = oidcJWKCacheEntry{algorithm: encoded.Algorithm, key: key}
		}
	}
	if len(keys) == 0 {
		return errors.New("OIDC JWKS contains no supported keys")
	}
	resolver.keys = keys
	resolver.fetchedAt = resolver.now()
	return nil
}

func (resolver *OIDCJWKResolver) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := resolver.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.Request == nil {
		return errors.New("OIDC endpoint response is invalid")
	}
	if _, err := validateOIDCRemoteURL(response.Request.URL.String()); err != nil {
		return errors.New("OIDC endpoint redirected to an invalid URL")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxOIDCMetadataBytes))
		return fmt.Errorf("OIDC endpoint returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxOIDCMetadataBytes+1))
	if err != nil || int64(len(data)) > maxOIDCMetadataBytes {
		return errors.New("OIDC metadata response is invalid")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return errors.New("OIDC metadata response is invalid")
	}
	return nil
}

func validateOIDCRemoteURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid OIDC URL")
	}
	if parsed.Scheme == "https" {
		return parsed, nil
	}
	if parsed.Scheme == "http" && isOIDCLoopback(parsed.Hostname()) {
		return parsed, nil
	}
	return nil, errors.New("invalid OIDC URL")
}

func isOIDCLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parseOIDCJWK(keyType, curve, algorithm, x, modulus, exponent string) (crypto.PublicKey, error) {
	switch {
	case keyType == "OKP" && curve == "Ed25519" && algorithm == "EdDSA":
		value, err := base64.RawURLEncoding.DecodeString(x)
		if err != nil || len(value) != ed25519.PublicKeySize {
			return nil, errors.New("invalid Ed25519 JWK")
		}
		return ed25519.PublicKey(value), nil
	case keyType == "RSA" && algorithm == "RS256":
		n, err := base64.RawURLEncoding.DecodeString(modulus)
		if err != nil || len(n) < 256 {
			return nil, errors.New("invalid RSA JWK")
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(exponent)
		if err != nil || len(eBytes) == 0 || len(eBytes) > 4 {
			return nil, errors.New("invalid RSA JWK")
		}
		e := 0
		for _, value := range eBytes {
			e = e<<8 | int(value)
		}
		if e < 3 || e%2 == 0 {
			return nil, errors.New("invalid RSA JWK")
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: e}, nil
	default:
		return nil, errors.New("unsupported JWK")
	}
}
