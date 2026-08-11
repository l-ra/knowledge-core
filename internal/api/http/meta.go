package apihttp

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/domain"
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
		Actor:          actor,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		CorrelationID:  corr,
		OperationType:  operationType,
		RequestHash:    bodyHash,
		ValidationMode: mode,
	}
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
