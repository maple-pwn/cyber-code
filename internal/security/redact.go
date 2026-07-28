// Package security provides shared trust-boundary hardening helpers.
package security

import (
	"errors"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

const redacted = "[REDACTED]"

var (
	bearerPattern               = regexp.MustCompile(`(?i)\bbearer\s+[^\s,;]+`)
	credentialAssignmentPattern = regexp.MustCompile(
		`(?i)\b(api[_-]?key|authorization|proxy-authorization|access[_-]?token|cookie|set-cookie)(\s*[:=]\s*)([^\s,;]+)`,
	)
	openAITokenPattern = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{4,}\b`)
)

type Redactor struct {
	secrets []string
}

func NewRedactor(secrets ...string) Redactor {
	unique := make(map[string]struct{}, len(secrets))
	filtered := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		if _, exists := unique[secret]; exists {
			continue
		}
		unique[secret] = struct{}{}
		filtered = append(filtered, secret)
	}
	sort.Slice(filtered, func(left, right int) bool { return len(filtered[left]) > len(filtered[right]) })
	return Redactor{secrets: filtered}
}

func (redactor Redactor) Text(value string) string {
	for _, secret := range redactor.secrets {
		value = strings.ReplaceAll(value, secret, redacted)
	}
	value = bearerPattern.ReplaceAllString(value, "Bearer "+redacted)
	value = credentialAssignmentPattern.ReplaceAllString(value, "$1$2"+redacted)
	return openAITokenPattern.ReplaceAllString(value, redacted)
}

func (redactor Redactor) Error(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(redactor.Text(err.Error()))
}

func (redactor Redactor) Headers(headers http.Header) http.Header {
	cloned := headers.Clone()
	for name, values := range cloned {
		if sensitiveHeader(name) {
			cloned[name] = []string{redacted}
			continue
		}
		for index, value := range values {
			values[index] = redactor.Text(value)
		}
	}
	return cloned
}

func sensitiveHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "x-api-key", "api-key", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}
