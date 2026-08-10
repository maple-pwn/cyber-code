package marketplace

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type CatalogSignature struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Signature string `json:"signature"`
}

func SignCatalog(catalog Catalog, keyID string, privateKey ed25519.PrivateKey) (CatalogSignature, error) {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" || len(privateKey) != ed25519.PrivateKeySize {
		return CatalogSignature{}, fmt.Errorf("catalog signing key and key ID are required")
	}
	payload, err := canonicalCatalog(catalog)
	if err != nil {
		return CatalogSignature{}, err
	}
	signature := ed25519.Sign(privateKey, payload)
	return CatalogSignature{Algorithm: "Ed25519", KeyID: keyID, Signature: base64.StdEncoding.EncodeToString(signature)}, nil
}

func VerifyCatalogSignature(catalog Catalog, signature CatalogSignature, trustedKeys map[string]ed25519.PublicKey) error {
	if signature.Algorithm != "Ed25519" || strings.TrimSpace(signature.KeyID) == "" {
		return fmt.Errorf("unsupported marketplace signature")
	}
	publicKey := trustedKeys[signature.KeyID]
	if len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("marketplace signature key %q is not trusted", signature.KeyID)
	}
	decoded, err := base64.StdEncoding.DecodeString(signature.Signature)
	if err != nil || len(decoded) != ed25519.SignatureSize {
		return fmt.Errorf("invalid marketplace signature encoding")
	}
	payload, err := canonicalCatalog(catalog)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, decoded) {
		return fmt.Errorf("marketplace signature verification failed")
	}
	return nil
}

func canonicalCatalog(catalog Catalog) ([]byte, error) {
	payload, err := json.Marshal(catalog)
	if err != nil {
		return nil, fmt.Errorf("encode marketplace catalog: %w", err)
	}
	return payload, nil
}
