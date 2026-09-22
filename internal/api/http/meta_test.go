package apihttp

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/l-ra/knowledge-core/internal/store"
)

func TestReadBodyLimited(t *testing.T) {
	t.Parallel()

	ok := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("abcd"))
	got, err := readBodyLimited(ok, 4)
	if err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	if string(got) != "abcd" {
		t.Fatalf("got %q", got)
	}

	tooBig := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte("x"), 5)))
	_, err = readBodyLimited(tooBig, 4)
	if !errors.Is(err, errPayloadTooLarge) {
		t.Fatalf("want payload too large, got %v", err)
	}
}

func TestImportReleasePayloadTooLargeHTTP(t *testing.T) {
	t.Parallel()
	s := &Server{}
	body := bytes.Repeat([]byte("a"), store.MaxReleaseBundleBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/v1/releases/import", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.importRelease(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "payload_too_large") {
		t.Fatalf("expected payload_too_large: %s", rec.Body.String())
	}
}
