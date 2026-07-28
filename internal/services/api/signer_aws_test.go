package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAWSSignerProducesDeterministicBedrockSignature(t *testing.T) {
	credentials := &AWSCredentials{AccessKeyID: "AKIDEXAMPLE", SecretAccessKey: "secret", SessionToken: "session"}
	signer := NewAWSSigner(credentials, "us-east-1")
	signer.now = func() time.Time { return time.Date(2026, time.July, 28, 3, 4, 5, 0, time.UTC) }
	request, err := http.NewRequest(http.MethodPost, "https://bedrock-runtime.us-east-1.amazonaws.com/model/claude/invoke", strings.NewReader(`{"prompt":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if err := signer.SignRequest(request); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if got := request.Header.Get("X-Amz-Date"); got != "20260728T030405Z" {
		t.Fatalf("x-amz-date = %q", got)
	}
	if got := request.Header.Get("X-Amz-Content-Sha256"); got != "8a44725210b9dcd4fefd9f0eca07b70ae45e69274a3105fb25eb426a2cf8bbf4" {
		t.Fatalf("payload hash = %q", got)
	}
	wantAuthorization := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20260728/us-east-1/bedrock/aws4_request, SignedHeaders=content-type;host;x-amz-content-sha256;x-amz-date;x-amz-security-token, Signature=c263b24b46f60a6480e017e76ddf9e2fd50ee10c2298ddecf990dc2ca1e26b0c"
	if got := request.Header.Get("Authorization"); got != wantAuthorization {
		t.Fatalf("authorization = %q", got)
	}
	body, err := io.ReadAll(request.Body)
	if err != nil || string(body) != `{"prompt":"hello"}` {
		t.Fatalf("signing consumed body: %q, %v", body, err)
	}
}
