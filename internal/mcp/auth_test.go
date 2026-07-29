package mcp

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestCredentialStorePersistsAndNeverIncludesSecretInErrors(t *testing.T) {
	store, err := NewCredentialStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(context.Background(), "server", Credential{AccessToken: "token-secret", RefreshToken: "refresh-secret", ExpiresAt: 123}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewCredentialStore(store.directory)
	if err != nil {
		t.Fatal(err)
	}
	credential, ok, err := reopened.Get(context.Background(), "server")
	if err != nil || !ok || credential.AccessToken != "token-secret" {
		t.Fatalf("credential=%#v ok=%v err=%v", credential, ok, err)
	}
	if _, _, err := reopened.Get(context.Background(), "missing"); err != nil && strings.Contains(err.Error(), "token-secret") {
		t.Fatal("secret leaked in error")
	}
	if info, err := os.Stat(store.path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("credential file mode = %v, err=%v", info.Mode().Perm(), err)
	}
	if info, err := os.Stat(store.directory); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("credential directory mode = %v, err=%v", info.Mode().Perm(), err)
	}
	if err := reopened.Delete(context.Background(), "server"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := reopened.Get(context.Background(), "server"); err != nil || ok {
		t.Fatalf("deleted credential = ok=%v err=%v", ok, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reopened.Put(ctx, "server", Credential{AccessToken: "secret"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled put error = %v", err)
	}
}
