package apihttp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/store"
)

func writeMetaFromRequest(r *http.Request, operationType string, bodyHash string) domain.WriteMeta {
	actor := r.Header.Get("X-Actor")
	if actor == "" {
		if sub, ok := auth.SubjectFromContext(r.Context()); ok {
			actor = sub.ID
		}
	}
	if actor == "" {
		actor = "system"
	}
	corr := r.Header.Get("X-Correlation-Id")
	if corr == "" {
		corr = middleware.GetReqID(r.Context())
	}
	mode := domain.ParseValidationMode(r.Header.Get("X-Validation-Mode"))
	if q := r.URL.Query().Get("validation"); q != "" {
		mode = domain.ParseValidationMode(q)
	}
	return domain.WriteMeta{
		Actor:           actor,
		IdempotencyKey:  r.Header.Get("Idempotency-Key"),
		CorrelationID:   corr,
		OperationType:   operationType,
		RequestHash:     bodyHash,
		ValidationMode:  mode,
		OpenChangeSetID: strings.TrimSpace(r.Header.Get("X-Knowledge-Changeset")),
	}
}

// rejectIfOpenChangeSet returns true and writes 400 when an open-CS header is present
// for an operation that must not silently write to committed tables.
func rejectIfOpenChangeSet(w http.ResponseWriter, meta domain.WriteMeta, op string) bool {
	if meta.OpenChangeSetID == "" {
		return false
	}
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"error": map[string]any{
			"code":    "unsupported_in_open_changeset",
			"message": fmt.Sprintf("%s cannot run inside an open ChangeSet (%s); omit X-Knowledge-Changeset or use a supported graph write", op, meta.OpenChangeSetID),
		},
	})
	return true
}

func withActiveChangeSets(r *http.Request) *http.Request {
	raw := strings.TrimSpace(r.Header.Get("X-Knowledge-Changesets"))
	if raw == "" {
		return r
	}
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			ids = append(ids, p)
		}
	}
	if len(ids) == 0 {
		return r
	}
	return r.WithContext(store.WithActiveChangeSets(r.Context(), ids))
}

func hashBody(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}
