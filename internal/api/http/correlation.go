package apihttp

import (
	"net/http"

	"github.com/go-chi/chi/v5/middleware"
)

func correlationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corr := r.Header.Get("X-Correlation-Id")
		if corr == "" {
			corr = middleware.GetReqID(r.Context())
		}
		if corr != "" {
			w.Header().Set("X-Correlation-Id", corr)
			w.Header().Set("X-Request-Id", middleware.GetReqID(r.Context()))
		}
		next.ServeHTTP(w, r)
	})
}
