package api

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAzureServicePrincipalRequestUsesInjectedEndpoint(t *testing.T) {
	t.Setenv("AZURE_TENANT_ID", "tenant")
	t.Setenv("AZURE_CLIENT_ID", "client")
	t.Setenv("AZURE_CLIENT_SECRET", "secret")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if request.Form.Get("grant_type") != "client_credentials" || request.Form.Get("client_id") != "client" || request.Form.Get("client_secret") != "secret" {
			t.Errorf("unexpected form: %v", request.Form)
		}
		if request.Form.Get("scope") != azureCognitiveServicesResource {
			t.Errorf("scope = %q", request.Form.Get("scope"))
		}
		_, _ = io.WriteString(response, `{"access_token":"azure-token","token_type":"Bearer","expires_in":3600}`)
	}))
	defer server.Close()
	manager := NewAzureAuthManager(AzureAuthOptions{HTTPClient: server.Client(), ServicePrincipalURL: server.URL, Now: fixedAzureTime})
	credentials, err := manager.getServicePrincipalToken(context.Background())
	if err != nil || credentials.AccessToken != "azure-token" {
		t.Fatalf("credentials = %#v, %v", credentials, err)
	}
}

func TestAzureManagedIdentityRequestUsesMetadataHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Metadata") != "true" {
			t.Errorf("Metadata = %q", request.Header.Get("Metadata"))
		}
		if request.URL.Query().Get("api-version") != "2018-02-01" || request.URL.Query().Get("resource") != "https://cognitiveservices.azure.com" {
			t.Errorf("query = %v", request.URL.Query())
		}
		_, _ = io.WriteString(response, `{"access_token":"managed-token","token_type":"Bearer","expires_in":"3600","resource":"cognitive"}`)
	}))
	defer server.Close()
	manager := NewAzureAuthManager(AzureAuthOptions{HTTPClient: server.Client(), ManagedIdentityURL: server.URL, Now: fixedAzureTime})
	credentials, err := manager.getManagedIdentityToken(context.Background())
	if err != nil || credentials.AccessToken != "managed-token" {
		t.Fatalf("credentials = %#v, %v", credentials, err)
	}
}

func fixedAzureTime() time.Time { return time.Date(2026, 7, 28, 5, 0, 0, 0, time.UTC) }
