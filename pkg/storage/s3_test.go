package storage

import (
	"context"
	"testing"
)

func TestNewS3AndPublicURL(t *testing.T) {
	client, err := NewS3("localhost:9000", "access", "secret", "avatars", false, "http://localhost:9000/avatars")
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	if got := client.PublicURL("avatars/id/original.png"); got != "http://localhost:9000/avatars/avatars/id/original.png" {
		t.Fatalf("PublicURL() = %q", got)
	}
	if got := client.PublicURL(""); got != "" {
		t.Fatalf("empty PublicURL() = %q", got)
	}
}

func TestDeleteManyWithNoKeysIsNoop(t *testing.T) {
	client, err := NewS3("localhost:9000", "access", "secret", "avatars", false, ":// bad")
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	if got := client.PublicURL("key"); got != ":// bad/key" {
		t.Fatalf("fallback PublicURL() = %q", got)
	}
	if err := client.DeleteMany(context.Background(), nil); err != nil {
		t.Fatalf("DeleteMany(nil) error = %v", err)
	}
}
