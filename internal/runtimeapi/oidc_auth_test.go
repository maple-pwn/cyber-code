package runtimeapi

import (
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestOIDCAuthenticatorVerifiesEdDSAClaimsAndRejectsAudience(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	authenticator, err := NewOIDCRemoteAuthenticator("https://issuer.example.test", "cyber-code", func(context.Context, string, string) (crypto.PublicKey, error) {
		return publicKey, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	authenticator.clock = func() time.Time { return clock }
	token := signOIDCTestToken(t, privateKey, map[string]any{"iss": "https://issuer.example.test", "aud": []string{"cyber-code"}, "sub": "owner@example.test", "exp": clock.Add(time.Hour).Unix(), "tenant_id": "tenant-a", "sid": "session-a", "role": "owner", "task_context_id": "context-a", "controller_id": "controller-a", "capabilities": []string{"events"}})
	claims, err := authenticator.Authenticate(context.Background(), token)
	if err != nil || claims.Principal != "owner@example.test" || claims.TenantID != "tenant-a" {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
	bad := token[:len(token)-1] + "A"
	if _, err := authenticator.Authenticate(context.Background(), bad); err == nil {
		t.Fatal("accepted token with altered audience")
	}
}

func signOIDCTestToken(t *testing.T, privateKey ed25519.PrivateKey, claims map[string]any) string {
	t.Helper()
	encode := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(data)
	}
	header := encode(map[string]string{"alg": "EdDSA", "kid": "test-key", "typ": "JWT"})
	body := encode(claims)
	signingInput := header + "." + body
	signature := ed25519.Sign(privateKey, []byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}
