package apihttp

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"

	"github.com/rasekl/knowledge-core/internal/auth"
	"github.com/rasekl/knowledge-core/internal/domain"
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
	return domain.WriteMeta{
		Actor:          actor,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
		CorrelationID:  r.Header.Get("X-Correlation-Id"),
		OperationType:  operationType,
		RequestHash:    bodyHash,
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
