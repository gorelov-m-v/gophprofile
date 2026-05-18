package handlers

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/internal/services"
)

func TestUploadAvatarReturnsCreated(t *testing.T) {
	fake := &handlerFakeService{}
	router := testRouter(New(fake, 10<<20, ""))

	body, contentType := multipartBody(t, "file", "avatar.png", handlerPNG(t))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", body)
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-User-ID", "user-123")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if fake.uploadInput.UserID != "user-123" || fake.uploadInput.FileName != "avatar.png" {
		t.Fatalf("upload input = %+v", fake.uploadInput)
	}
	if !strings.Contains(rec.Body.String(), `"status":"processing"`) {
		t.Fatalf("response body = %s", rec.Body.String())
	}
}

func TestUploadAvatarRequiresFile(t *testing.T) {
	router := testRouter(New(&handlerFakeService{}, 10<<20, ""))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars", strings.NewReader("--bad"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=bad")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGetAvatarWritesImageHeaders(t *testing.T) {
	router := testRouter(New(&handlerFakeService{}, 10<<20, ""))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/avatar-id?size=100x100", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("ETag") != `"abc"` {
		t.Fatalf("etag = %q", rec.Header().Get("ETag"))
	}
}

func TestGetAvatarRejectsInvalidFormat(t *testing.T) {
	router := testRouter(New(&handlerFakeService{}, 10<<20, ""))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/avatar-id?format=gif", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestUserAvatarMetadataListDeleteAndHealthRoutes(t *testing.T) {
	router := testRouter(New(&handlerFakeService{}, 10<<20, ""))

	cases := []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/api/v1/users/user-123/avatar", http.StatusOK},
		{http.MethodGet, "/api/v1/avatars/avatar-id/metadata", http.StatusOK},
		{http.MethodGet, "/api/v1/users/user-123/avatars", http.StatusOK},
		{http.MethodDelete, "/api/v1/users/user-123/avatar", http.StatusNoContent},
		{http.MethodGet, "/health", http.StatusOK},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-User-ID", "user-123")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%s %s status = %d, body = %s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

func TestWebUploadPostRedirectsToGallery(t *testing.T) {
	router := testRouter(New(&handlerFakeService{}, 10<<20, ""))
	body, contentType := multipartBody(t, "file", "avatar.png", handlerPNG(t))
	req := httptest.NewRequest(http.MethodPost, "/web/upload?user_id=user-123", body)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/web/gallery/user-123" {
		t.Fatalf("location = %q", got)
	}
}

func TestDeleteAvatarMapsForbidden(t *testing.T) {
	router := testRouter(New(&handlerFakeService{deleteErr: domain.ErrForbidden}, 10<<20, ""))
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/avatars/avatar-id", nil)
	req.Header.Set("X-User-ID", "other")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func testRouter(handler *Handler) http.Handler {
	r := chi.NewRouter()
	r.Post("/api/v1/avatars", handler.UploadAvatar)
	r.Get("/api/v1/avatars/{avatar_id}", handler.GetAvatar)
	r.Delete("/api/v1/avatars/{avatar_id}", handler.DeleteAvatar)
	r.Get("/api/v1/users/{user_id}/avatar", handler.GetUserAvatar)
	r.Get("/api/v1/avatars/{avatar_id}/metadata", handler.GetMetadata)
	r.Get("/api/v1/users/{user_id}/avatars", handler.ListUserAvatars)
	r.Delete("/api/v1/users/{user_id}/avatar", handler.DeleteUserAvatar)
	r.Get("/health", handler.Health)
	r.Post("/web/upload", handler.WebUploadPost)
	return r
}

type handlerFakeService struct {
	uploadInput services.UploadInput
	deleteErr   error
}

func (s *handlerFakeService) Upload(_ context.Context, input services.UploadInput) (*domain.Avatar, error) {
	s.uploadInput = input
	return &domain.Avatar{
		ID:               "avatar-id",
		UserID:           input.UserID,
		FileName:         input.FileName,
		MimeType:         "image/png",
		SizeBytes:        int64(len(input.Data)),
		ProcessingStatus: domain.ProcessingStatusProcessing,
		CreatedAt:        time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (s *handlerFakeService) GetImage(context.Context, string, string, string) (services.ImageObject, error) {
	return services.ImageObject{Data: []byte("png"), ContentType: "image/png", ETag: "abc"}, nil
}

func (s *handlerFakeService) GetLatestUserImage(context.Context, string, string, string) (services.ImageObject, error) {
	return services.ImageObject{Data: []byte("png"), ContentType: "image/png", ETag: "abc"}, nil
}

func (s *handlerFakeService) GetMetadata(context.Context, string) (*domain.Avatar, error) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	return &domain.Avatar{
		ID:        "avatar-id",
		UserID:    "user-123",
		FileName:  "avatar.png",
		MimeType:  "image/png",
		SizeBytes: 123,
		Width:     10,
		Height:    10,
		ThumbnailS3Keys: map[string]string{
			"100x100": "thumb.jpg",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *handlerFakeService) ListUserAvatars(context.Context, string) ([]domain.Avatar, error) {
	avatar, _ := s.GetMetadata(context.Background(), "avatar-id")
	return []domain.Avatar{*avatar}, nil
}

func (s *handlerFakeService) Delete(context.Context, string, string) error {
	return s.deleteErr
}

func (s *handlerFakeService) DeleteLatestByUser(context.Context, string, string) error {
	return s.deleteErr
}

func (s *handlerFakeService) Health(context.Context) map[string]string {
	return map[string]string{"database": "ok", "s3": "ok", "rabbitmq": "ok"}
}

func (s *handlerFakeService) URLForAvatar(id string) string {
	return "/api/v1/avatars/" + id
}

func (s *handlerFakeService) URLForS3Key(key string) string {
	return "/objects/" + key
}

func multipartBody(t *testing.T, field, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile(field, filename)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return body, writer.FormDataContentType()
}

func handlerPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 20, G: 30, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
