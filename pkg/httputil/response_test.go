package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONAndErrorResponses(t *testing.T) {
	rec := httptest.NewRecorder()
	JSON(rec, http.StatusAccepted, map[string]string{"ok": "yes"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("JSON status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), `"ok":"yes"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	Error(rec, http.StatusBadRequest, "Invalid", "details")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"details":"details"`) {
		t.Fatalf("error response = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	TooLarge(rec, 10)
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"max_size":10`) {
		t.Fatalf("too large response = %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	NoContent(rec)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("NoContent status = %d", rec.Code)
	}
}
