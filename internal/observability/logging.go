package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

type traceHandler struct {
	next slog.Handler
}

func NewLogger(serviceName, levelName, logFile string) (*slog.Logger, func() error, error) {
	level := slog.LevelInfo
	switch strings.ToLower(levelName) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	writer := io.Writer(os.Stdout)
	var file *os.File
	if logFile != "" {
		var err error
		if err = os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
			return nil, nil, err
		}
		file, err = os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, nil, err
		}
		writer = io.MultiWriter(os.Stdout, file)
	}

	handler := &traceHandler{next: slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level})}
	logger := slog.New(handler).With("service", serviceName)
	closeFn := func() error {
		if file == nil {
			return nil
		}
		return file.Close()
	}
	return logger, closeFn, nil
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, record slog.Record) error {
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", spanContext.TraceID().String()),
			slog.String("span_id", spanContext.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{next: h.next.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{next: h.next.WithGroup(name)}
}
