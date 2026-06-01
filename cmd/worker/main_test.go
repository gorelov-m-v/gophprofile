package main

import (
	"context"
	"errors"
	"testing"
)

func TestWithRetryEventuallySucceeds(t *testing.T) {
	attempts := 0
	err := withRetry(context.Background(), func() error {
		attempts++
		if attempts < 2 {
			return errors.New("temporary")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("withRetry() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestWithRetryReturnsLastError(t *testing.T) {
	want := errors.New("still failing")
	err := withRetry(context.Background(), func() error {
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("withRetry() error = %v", err)
	}
}
