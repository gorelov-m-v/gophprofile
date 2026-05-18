package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/internal/handlers"
	"github.com/gorelov-m-v/gophprofile/internal/services"
)

func TestNewRouterRegistersHealthAndRedirect(t *testing.T) {
	router := NewRouter(handlers.New(&routerFakeService{}, 10<<20, ""), "")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/health status = %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("/ status = %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/web/upload" {
		t.Fatalf("redirect location = %q", got)
	}
}

type routerFakeService struct{}

func (s *routerFakeService) Upload(context.Context, services.UploadInput) (*domain.Avatar, error) {
	return &domain.Avatar{}, nil
}

func (s *routerFakeService) GetImage(context.Context, string, string, string) (services.ImageObject, error) {
	return services.ImageObject{}, nil
}

func (s *routerFakeService) GetLatestUserImage(context.Context, string, string, string) (services.ImageObject, error) {
	return services.ImageObject{}, nil
}

func (s *routerFakeService) GetMetadata(context.Context, string) (*domain.Avatar, error) {
	return &domain.Avatar{}, nil
}

func (s *routerFakeService) ListUserAvatars(context.Context, string) ([]domain.Avatar, error) {
	return nil, nil
}

func (s *routerFakeService) Delete(context.Context, string, string) error {
	return nil
}

func (s *routerFakeService) DeleteLatestByUser(context.Context, string, string) error {
	return nil
}

func (s *routerFakeService) Health(context.Context) map[string]string {
	return map[string]string{"database": "ok", "s3": "ok", "rabbitmq": "ok"}
}

func (s *routerFakeService) URLForAvatar(string) string {
	return ""
}

func (s *routerFakeService) URLForS3Key(string) string {
	return ""
}
