package observability

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceHandlerAddsTraceFields(t *testing.T) {
	var buf bytes.Buffer
	handler := &traceHandler{next: slog.NewJSONHandler(&buf, nil)}
	logger := slog.New(handler)

	traceID, err := trace.TraceIDFromHex("11111111111111111111111111111111")
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("2222222222222222")
	if err != nil {
		t.Fatalf("span id: %v", err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	logger.InfoContext(ctx, "hello")

	out := buf.String()
	if !strings.Contains(out, `"trace_id":"11111111111111111111111111111111"`) {
		t.Fatalf("missing trace id in %s", out)
	}
	if !strings.Contains(out, `"span_id":"2222222222222222"`) {
		t.Fatalf("missing span id in %s", out)
	}
}

func TestNewLoggerWritesToFile(t *testing.T) {
	path := t.TempDir() + "/app.log"
	logger, closeFn, err := NewLogger("test-service", "debug", path)
	if err != nil {
		t.Fatalf("NewLogger() error = %v", err)
	}
	logger.DebugContext(context.Background(), "debug message")
	if err := closeFn(); err != nil {
		t.Fatalf("close logger: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !strings.Contains(string(data), `"service":"test-service"`) {
		t.Fatalf("missing service in %s", data)
	}
}
