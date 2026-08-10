package marketplace

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestCatalogSignatureVerifiesTrustedKeyRotationAndRejectsTampering(t *testing.T) {
	oldPublic, oldPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	newPublic, newPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	catalog := Catalog{Name: "signed-market", Version: "1.0.0", Plugins: []PluginEntry{{Name: "review-kit", Version: "1.2.3", Source: "plugins/review-kit", Digest: "abc123"}}}
	oldSignature, err := SignCatalog(catalog, "old", oldPrivate)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]ed25519.PublicKey{"old": oldPublic, "new": newPublic}
	if err := VerifyCatalogSignature(catalog, oldSignature, keys); err != nil {
		t.Fatal(err)
	}
	newSignature, err := SignCatalog(catalog, "new", newPrivate)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyCatalogSignature(catalog, newSignature, keys); err != nil {
		t.Fatal(err)
	}
	tampered := catalog
	tampered.Plugins = append([]PluginEntry(nil), catalog.Plugins...)
	tampered.Plugins[0].Version = "9.9.9"
	if err := VerifyCatalogSignature(tampered, newSignature, keys); err == nil {
		t.Fatal("tampered catalog signature was accepted")
	}
}
