package security

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestRedactorRemovesKnownSecretsAndCredentialAssignments(t *testing.T) {
	secret := "known-secret-value"
	redactor := NewRedactor(secret)
	input := "raw=" + secret + " Authorization: Bearer bearer-value api_key=key-value Cookie: session=cookie-value"

	got := redactor.Text(input)
	for _, leaked := range []string{secret, "bearer-value", "key-value", "cookie-value"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted text contains %q: %s", leaked, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redacted text = %q", got)
	}
}

func TestRedactorCopiesHeadersAndRedactsSensitiveValues(t *testing.T) {
	headers := http.Header{
		"Authorization": {"Bearer auth-secret"},
		"Cookie":        {"session=cookie-secret"},
		"X-Api-Key":     {"api-secret"},
		"Content-Type":  {"application/json"},
	}
	got := NewRedactor().Headers(headers)
	if got.Get("Authorization") != "[REDACTED]" || got.Get("Cookie") != "[REDACTED]" || got.Get("X-Api-Key") != "[REDACTED]" {
		t.Fatalf("sensitive headers = %#v", got)
	}
	if got.Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", got.Get("Content-Type"))
	}
	if headers.Get("Authorization") != "Bearer auth-secret" {
		t.Fatal("input headers were mutated")
	}
}

func TestRedactorSanitizesErrors(t *testing.T) {
	secret := "transport-secret"
	got := NewRedactor(secret).Error(errors.New("request failed with " + secret))
	if got == nil || strings.Contains(got.Error(), secret) {
		t.Fatalf("redacted error = %v", got)
	}
	if NewRedactor().Error(nil) != nil {
		t.Fatal("nil error was not preserved")
	}
}

func TestRedactorDoesNotRewriteOrdinarySourceIdentifiers(t *testing.T) {
	input := "let secret = config.value\nlet token: string = parse(input)"
	if got := NewRedactor().Text(input); got != input {
		t.Fatalf("ordinary source was rewritten: %q", got)
	}
}

func TestRedactorIgnoresEmptyAndDuplicateSecrets(t *testing.T) {
	redactor := NewRedactor("", "same-secret", "same-secret")
	if got := redactor.Text("same-secret"); got != redacted {
		t.Fatalf("redacted text = %q", got)
	}
}
