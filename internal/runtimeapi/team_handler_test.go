package runtimeapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTeamHandlerRoutesRuntimeAndAdminExactly(t *testing.T) {
	runtimeCalls, adminCalls := 0, 0
	handler, err := NewTeamHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { runtimeCalls++; w.WriteHeader(http.StatusNoContent) }), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { adminCalls++; w.WriteHeader(http.StatusAccepted) }))
	if err != nil {
		t.Fatal(err)
	}
	for path, status := range map[string]int{"/runtime": http.StatusNoContent, "/admin": http.StatusAccepted, "/runtime/forged": http.StatusNotFound, "/": http.StatusNotFound} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != status {
			t.Fatalf("%s status = %d, want %d", path, response.Code, status)
		}
	}
	if runtimeCalls != 1 || adminCalls != 1 {
		t.Fatalf("calls runtime=%d admin=%d", runtimeCalls, adminCalls)
	}
}

func TestTeamHandlerRequiresBothSecurityBoundaries(t *testing.T) {
	if _, err := NewTeamHandler(nil, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})); err == nil {
		t.Fatal("accepted missing runtime handler")
	}
	if _, err := NewTeamHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), nil); err == nil {
		t.Fatal("accepted missing admin handler")
	}
}
