package broker

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorelov-m-v/gophprofile/internal/domain"
	amqp "github.com/rabbitmq/amqp091-go"
)

func TestRetryBackoffBoundsAttempts(t *testing.T) {
	if got := RetryBackoff(-1); got != 200*time.Millisecond {
		t.Fatalf("negative backoff = %v", got)
	}
	if got := RetryBackoff(2); got != 800*time.Millisecond {
		t.Fatalf("attempt 2 backoff = %v", got)
	}
	if got := RetryBackoff(100); got != 12800*time.Millisecond {
		t.Fatalf("bounded backoff = %v", got)
	}
}

func TestRetryCountReadsSupportedHeaderTypes(t *testing.T) {
	cases := []struct {
		value any
		want  int
	}{
		{int(2), 2},
		{int32(3), 3},
		{int64(4), 4},
		{"5", 5},
		{true, 0},
	}
	for _, tc := range cases {
		if got := retryCount(amqp.Table{retryHeader: tc.value}); got != tc.want {
			t.Fatalf("retryCount(%T) = %d, want %d", tc.value, got, tc.want)
		}
	}
	if got := retryCount(nil); got != 0 {
		t.Fatalf("nil retryCount = %d", got)
	}
}

func TestRabbitMQNilConnectionHelpers(t *testing.T) {
	r := &RabbitMQ{}
	if err := r.Ping(); err == nil {
		t.Fatal("expected ping error for nil connection")
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewRabbitMQRejectsInvalidURL(t *testing.T) {
	if _, err := NewRabbitMQ(":// bad", "avatars.exchange"); err == nil {
		t.Fatal("expected invalid rabbitmq URL error")
	}
}

func TestPublishRejectsNilChannel(t *testing.T) {
	r := &RabbitMQ{}
	if err := r.PublishUpload(context.Background(), domain.AvatarUploadEvent{AvatarID: "avatar-id"}); err == nil {
		t.Fatal("expected PublishUpload error")
	}
	if err := r.PublishDelete(context.Background(), domain.AvatarDeleteEvent{AvatarID: "avatar-id"}); err == nil {
		t.Fatal("expected PublishDelete error")
	}
}

func TestEnsureMessageIDPreservesCallerID(t *testing.T) {
	if got := ensureMessageID("event-message-id"); got != "event-message-id" {
		t.Fatalf("message id = %q", got)
	}
}

func TestEnsureMessageIDGeneratesMissingID(t *testing.T) {
	got := ensureMessageID("")
	if got == "" {
		t.Fatal("expected generated message id")
	}
	if _, err := uuid.Parse(got); err != nil {
		t.Fatalf("message id is not a uuid: %v", err)
	}
}
