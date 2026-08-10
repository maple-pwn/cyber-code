package authorization

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrPolicyInvalid  = errors.New("managed_policy_invalid")
	ErrPolicyRollback = errors.New("managed_policy_rollback")
)

type ManagedPolicy struct {
	Issuer       string    `json:"issuer"`
	TenantID     string    `json:"tenant_id"`
	Revision     uint64    `json:"revision"`
	IssuedAt     time.Time `json:"issued_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	TrustLevel   string    `json:"trust_level"`
	AllowedTools []string  `json:"allowed_tools"`
	DenyTools    []string  `json:"deny_tools"`
}

type SignedManagedPolicy struct {
	KeyID     string          `json:"key_id"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

type ManagedPolicyVerifierOptions struct {
	Issuer   string
	TenantID string
	Keys     map[string]ed25519.PublicKey
	Now      func() time.Time
}

type ManagedPolicyVerifier struct {
	issuer   string
	tenantID string
	keys     map[string]ed25519.PublicKey
	now      func() time.Time

	mu       sync.Mutex
	revision uint64
}

func NewManagedPolicyVerifier(options ManagedPolicyVerifierOptions) (*ManagedPolicyVerifier, error) {
	issuer, err := validateManagedPolicyIssuer(options.Issuer)
	if err != nil || !validProvisionText(options.TenantID) || len(options.Keys) == 0 {
		return nil, ErrPolicyInvalid
	}
	keys := make(map[string]ed25519.PublicKey, len(options.Keys))
	for keyID, key := range options.Keys {
		if !validProvisionText(keyID) || len(key) != ed25519.PublicKeySize {
			return nil, ErrPolicyInvalid
		}
		keys[keyID] = append(ed25519.PublicKey(nil), key...)
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &ManagedPolicyVerifier{issuer: issuer, tenantID: options.TenantID, keys: keys, now: now}, nil
}

func SignManagedPolicy(keyID string, privateKey ed25519.PrivateKey, policy ManagedPolicy) (SignedManagedPolicy, error) {
	if !validProvisionText(keyID) || len(privateKey) != ed25519.PrivateKeySize {
		return SignedManagedPolicy{}, ErrPolicyInvalid
	}
	payload, _, err := canonicalManagedPolicy(policy)
	if err != nil {
		return SignedManagedPolicy{}, err
	}
	signature := ed25519.Sign(privateKey, payload)
	return SignedManagedPolicy{KeyID: keyID, Payload: json.RawMessage(payload), Signature: base64.RawURLEncoding.EncodeToString(signature)}, nil
}

func (verifier *ManagedPolicyVerifier) VerifyAndInstall(data []byte) (ManagedPolicy, error) {
	if verifier == nil || len(data) == 0 || len(data) > 1<<20 {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var envelope SignedManagedPolicy
	if err := decoder.Decode(&envelope); err != nil || len(envelope.Payload) == 0 {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	key, ok := verifier.keys[envelope.KeyID]
	if !ok {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	signature, err := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if err != nil || base64.RawURLEncoding.EncodeToString(signature) != envelope.Signature || !ed25519.Verify(key, envelope.Payload, signature) {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	var policy ManagedPolicy
	if err := json.Unmarshal(envelope.Payload, &policy); err != nil {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	canonical, normalized, err := canonicalManagedPolicy(policy)
	if err != nil || !bytes.Equal(canonical, envelope.Payload) {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	now := verifier.now().UTC()
	if normalized.Issuer != verifier.issuer || normalized.TenantID != verifier.tenantID || normalized.IssuedAt.After(now.Add(5*time.Minute)) || !normalized.ExpiresAt.After(now) || normalized.ExpiresAt.After(now.Add(30*24*time.Hour)) {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	if normalized.Revision <= verifier.revision {
		return ManagedPolicy{}, ErrPolicyRollback
	}
	verifier.revision = normalized.Revision
	return normalized, nil
}

func canonicalManagedPolicy(policy ManagedPolicy) ([]byte, ManagedPolicy, error) {
	issuer, err := validateManagedPolicyIssuer(policy.Issuer)
	if err != nil || !validProvisionText(policy.TenantID) || policy.Revision == 0 || policy.TrustLevel != "managed" || policy.IssuedAt.IsZero() || !policy.ExpiresAt.After(policy.IssuedAt) {
		return nil, ManagedPolicy{}, ErrPolicyInvalid
	}
	allowed, err := normalizeManagedTools(policy.AllowedTools)
	if err != nil {
		return nil, ManagedPolicy{}, err
	}
	denied, err := normalizeManagedTools(policy.DenyTools)
	if err != nil {
		return nil, ManagedPolicy{}, err
	}
	policy.Issuer = issuer
	policy.IssuedAt = policy.IssuedAt.UTC()
	policy.ExpiresAt = policy.ExpiresAt.UTC()
	policy.AllowedTools = allowed
	policy.DenyTools = denied
	payload, err := json.Marshal(policy)
	if err != nil {
		return nil, ManagedPolicy{}, ErrPolicyInvalid
	}
	return payload, policy, nil
}

func normalizeManagedTools(values []string) ([]string, error) {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 128 || strings.ContainsAny(value, " \t\r\n") {
			return nil, ErrPolicyInvalid
		}
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func validateManagedPolicyIssuer(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("invalid managed policy issuer")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}
