package metrics

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMiddlewareRecordsRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestObserveHelpers(t *testing.T) {
	started := time.Now().Add(-time.Millisecond)

	ObserveUpload("user-1", 128, started, nil)
	ObserveUpload("user-1", 128, started, errors.New("failed"))
	ObserveDelete("user-1", 64)
	ObserveWorkerJob("upload", started, nil)
	ObserveWorkerJob("delete", started, errors.New("failed"))
}

func TestRegisterGaugeIgnoresDuplicateCollector(t *testing.T) {
	name := fmt.Sprintf("gophprofile_test_gauge_%d", time.Now().UnixNano())
	RegisterGauge(name, "test gauge", "test", func() float64 { return 1 })
	RegisterGauge(name, "test gauge", "test", func() float64 { return 1 })

	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range metricFamilies {
		if family.GetName() == name {
			return
		}
	}
	t.Fatalf("metric %s not registered", name)
}
