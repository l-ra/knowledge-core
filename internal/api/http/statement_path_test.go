package apihttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestStatementPathRequiresEncoding(t *testing.T) {
	const sid = "urn:kc:test:statement/s_example"

	r := chi.NewRouter()
	r.Get("/v1/statements/{sid}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	encoded := httptest.NewRequest(http.MethodGet, statementPath(sid, ""), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, encoded)
	if rec.Code != http.StatusOK {
		t.Fatalf("encoded path: status %d", rec.Code)
	}

	raw := httptest.NewRequest(http.MethodGet, "/v1/statements/"+sid, nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, raw)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("raw path with slash: status %d, want 404", rec.Code)
	}
}
