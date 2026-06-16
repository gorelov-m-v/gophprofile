package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type responseRecorder struct {
	http.ResponseWriter
	status int
}

var (
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_http_requests_total",
		Help: "Total HTTP requests.",
	}, []string{"method", "route", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gophprofile_http_request_duration_seconds",
		Help:    "HTTP request duration.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route", "status"})

	UploadsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "avatars_uploads_total",
		Help: "Total number of avatar uploads.",
	}, []string{"status", "user_id"})

	UploadDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "avatars_upload_duration_seconds",
		Help:    "Avatar upload duration.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"status"})

	StorageUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "avatars_storage_bytes",
		Help: "Total original avatar storage bytes by user.",
	}, []string{"user_id"})

	WorkerJobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "gophprofile_worker_jobs_total",
		Help: "Total worker jobs.",
	}, []string{"operation", "status"})

	WorkerJobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "gophprofile_worker_job_duration_seconds",
		Help:    "Worker job duration.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"operation", "status"})
)

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		route := ""
		if routeCtx := chi.RouteContext(r.Context()); routeCtx != nil {
			route = routeCtx.RoutePattern()
		}
		if route == "" {
			route = r.URL.Path
		}
		status := strconv.Itoa(recorder.status)
		HTTPRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		HTTPRequestDuration.WithLabelValues(r.Method, route, status).Observe(time.Since(start).Seconds())
	})
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func ObserveUpload(userID string, sizeBytes int64, started time.Time, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	UploadsTotal.WithLabelValues(status, userID).Inc()
	UploadDuration.WithLabelValues(status).Observe(time.Since(started).Seconds())
	if err == nil {
		StorageUsage.WithLabelValues(userID).Add(float64(sizeBytes))
	}
}

func ObserveDelete(userID string, sizeBytes int64) {
	StorageUsage.WithLabelValues(userID).Sub(float64(sizeBytes))
}

func ObserveWorkerJob(operation string, started time.Time, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	WorkerJobsTotal.WithLabelValues(operation, status).Inc()
	WorkerJobDuration.WithLabelValues(operation, status).Observe(time.Since(started).Seconds())
}

func RegisterGauge(name, help, service string, fn func() float64) {
	register(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name:        name,
		Help:        help,
		ConstLabels: prometheus.Labels{"service": service},
	}, fn))
}

func register(collector prometheus.Collector) {
	if err := prometheus.Register(collector); err != nil {
		if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
			panic(err)
		}
	}
}
