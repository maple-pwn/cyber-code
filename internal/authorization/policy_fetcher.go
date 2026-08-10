package authorization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type ManagedPolicyFetcher struct {
	endpoint string
	verifier *ManagedPolicyVerifier
	client   *http.Client
	now      func() time.Time

	mu      sync.Mutex
	current ManagedPolicy
	digest  string
	have    bool
}

func NewManagedPolicyFetcher(endpoint string, verifier *ManagedPolicyVerifier, client *http.Client, now func() time.Time) (*ManagedPolicyFetcher, error) {
	parsed, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || verifier == nil {
		return nil, ErrPolicyInvalid
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if now == nil {
		now = time.Now
	}
	return &ManagedPolicyFetcher{endpoint: parsed.String(), verifier: verifier, client: client, now: now}, nil
}

func (fetcher *ManagedPolicyFetcher) Fetch(ctx context.Context) (ManagedPolicy, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fetcher.endpoint, nil)
	if err != nil {
		return ManagedPolicy{}, ErrPolicyInvalid
	}
	request.Header.Set("Accept", "application/json")
	response, err := fetcher.client.Do(request)
	if err == nil {
		defer response.Body.Close()
		if response.Request == nil || response.Request.URL.Scheme != "https" || response.StatusCode < 200 || response.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			err = ErrPolicyInvalid
		} else {
			body, readErr := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
			if readErr != nil || len(body) > 1<<20 {
				err = ErrPolicyInvalid
			} else {
				digestBytes := sha256.Sum256(body)
				digest := hex.EncodeToString(digestBytes[:])
				if fetcher.have && digest == fetcher.digest && fetcher.current.ExpiresAt.After(fetcher.now().UTC()) {
					return fetcher.current, nil
				}
				policy, verifyErr := fetcher.verifier.VerifyAndInstall(body)
				if verifyErr == nil {
					fetcher.current, fetcher.digest, fetcher.have = policy, digest, true
					return policy, nil
				}
				err = verifyErr
			}
		}
	}
	if fetcher.have && fetcher.current.ExpiresAt.After(fetcher.now().UTC()) && err != nil {
		return fetcher.current, nil
	}
	return ManagedPolicy{}, err
}
