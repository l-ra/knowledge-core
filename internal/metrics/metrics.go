package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "kc_http_requests_total",
		Help: "HTTP requests by method, path group, and status",
	}, []string{"method", "path", "status"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "kc_http_request_duration_seconds",
		Help:    "HTTP request latency",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})

	OutboxProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "kc_outbox_processed_total",
		Help: "Outbox events processed",
	})
)

func Handler() http.Handler {
	return promhttp.Handler()
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		path := r.URL.Path
		if len(path) > 64 {
			path = path[:64]
		}
		HTTPRequests.WithLabelValues(r.Method, path, strconv.Itoa(rec.status)).Inc()
		HTTPDuration.WithLabelValues(r.Method, path).Observe(time.Since(start).Seconds())
	})
}
