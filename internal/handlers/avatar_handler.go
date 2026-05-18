package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/internal/services"
	"github.com/gorelov-m-v/gophprofile/pkg/httputil"
	"github.com/gorelov-m-v/gophprofile/pkg/imageutil"
)

type AvatarService interface {
	Upload(ctx context.Context, input services.UploadInput) (*domain.Avatar, error)
	GetImage(ctx context.Context, avatarID, size, format string) (services.ImageObject, error)
	GetLatestUserImage(ctx context.Context, userID, size, format string) (services.ImageObject, error)
	GetMetadata(ctx context.Context, avatarID string) (*domain.Avatar, error)
	ListUserAvatars(ctx context.Context, userID string) ([]domain.Avatar, error)
	Delete(ctx context.Context, avatarID, requestUserID string) error
	DeleteLatestByUser(ctx context.Context, pathUserID, requestUserID string) error
	Health(ctx context.Context) map[string]string
	URLForAvatar(id string) string
	URLForS3Key(key string) string
}

type Handler struct {
	service       AvatarService
	maxUploadSize int64
	webDir        string
}

func New(service AvatarService, maxUploadSize int64, webDir string) *Handler {
	return &Handler{service: service, maxUploadSize: maxUploadSize, webDir: webDir}
}

type uploadResponse struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	URL       string `json:"url"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type metadataResponse struct {
	ID         string             `json:"id"`
	UserID     string             `json:"user_id"`
	FileName   string             `json:"file_name"`
	MimeType   string             `json:"mime_type"`
	Size       int64              `json:"size"`
	Dimensions domain.Dimensions  `json:"dimensions"`
	Thumbnails []domain.Thumbnail `json:"thumbnails"`
	CreatedAt  string             `json:"created_at"`
	UpdatedAt  string             `json:"updated_at"`
}

func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID := strings.TrimSpace(r.Header.Get("X-User-ID"))
	data, fileName, ok := h.readUpload(w, r)
	if !ok {
		return
	}
	avatar, err := h.service.Upload(r.Context(), services.UploadInput{
		UserID:   userID,
		FileName: fileName,
		Data:     data,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, uploadResponse{
		ID:        avatar.ID,
		UserID:    avatar.UserID,
		URL:       h.service.URLForAvatar(avatar.ID),
		Status:    string(domain.ProcessingStatusProcessing),
		CreatedAt: avatar.CreatedAt.UTC().Format(time.RFC3339),
	})
}

func (h *Handler) GetAvatar(w http.ResponseWriter, r *http.Request) {
	avatarID := chi.URLParam(r, "avatar_id")
	size := r.URL.Query().Get("size")
	format := r.URL.Query().Get("format")
	if !validFormat(format) {
		httputil.Error(w, http.StatusBadRequest, "Invalid format", "Supported formats: jpeg, png, webp")
		return
	}
	obj, err := h.service.GetImage(r.Context(), avatarID, size, format)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeImage(w, obj)
}

func (h *Handler) GetUserAvatar(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	size := r.URL.Query().Get("size")
	format := r.URL.Query().Get("format")
	if !validFormat(format) {
		httputil.Error(w, http.StatusBadRequest, "Invalid format", "Supported formats: jpeg, png, webp")
		return
	}
	obj, err := h.service.GetLatestUserImage(r.Context(), userID, size, format)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeImage(w, obj)
}

func (h *Handler) GetMetadata(w http.ResponseWriter, r *http.Request) {
	avatar, err := h.service.GetMetadata(r.Context(), chi.URLParam(r, "avatar_id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, h.metadata(*avatar))
}

func (h *Handler) ListUserAvatars(w http.ResponseWriter, r *http.Request) {
	avatars, err := h.service.ListUserAvatars(r.Context(), chi.URLParam(r, "user_id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	resp := make([]metadataResponse, 0, len(avatars))
	for _, avatar := range avatars {
		resp = append(resp, h.metadata(avatar))
	}
	httputil.JSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	if err := h.service.Delete(r.Context(), chi.URLParam(r, "avatar_id"), r.Header.Get("X-User-ID")); err != nil {
		h.writeError(w, err)
		return
	}
	httputil.NoContent(w)
}

func (h *Handler) DeleteUserAvatar(w http.ResponseWriter, r *http.Request) {
	if err := h.service.DeleteLatestByUser(r.Context(), chi.URLParam(r, "user_id"), r.Header.Get("X-User-ID")); err != nil {
		h.writeError(w, err)
		return
	}
	httputil.NoContent(w)
}

func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	components := h.service.Health(r.Context())
	status := "ok"
	code := http.StatusOK
	for _, value := range components {
		if value != "ok" {
			status = "degraded"
			code = http.StatusServiceUnavailable
			break
		}
	}
	httputil.JSON(w, code, map[string]any{
		"status":     status,
		"components": components,
	})
}

func (h *Handler) WebUpload(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(h.webDir, "index.html"))
}

func (h *Handler) WebGallery(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(h.webDir, "gallery.html"))
}

func (h *Handler) WebUploadPost(w http.ResponseWriter, r *http.Request) {
	data, fileName, ok := h.readUpload(w, r)
	if !ok {
		return
	}
	userID := strings.TrimSpace(r.FormValue("user_id"))
	if userID == "" {
		userID = strings.TrimSpace(r.Header.Get("X-User-ID"))
	}
	avatar, err := h.service.Upload(r.Context(), services.UploadInput{
		UserID:   userID,
		FileName: fileName,
		Data:     data,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	http.Redirect(w, r, "/web/gallery/"+avatar.UserID, http.StatusSeeOther)
}

func (h *Handler) readUpload(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadSize+1024)
	if err := r.ParseMultipartForm(h.maxUploadSize); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "too large") || strings.Contains(err.Error(), "request body too large") {
			httputil.TooLarge(w, h.maxUploadSize)
			return nil, "", false
		}
		httputil.Error(w, http.StatusBadRequest, "Invalid multipart form", err.Error())
		return nil, "", false
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		file, header, err = r.FormFile("image")
	}
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "File is required")
		return nil, "", false
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, h.maxUploadSize+1))
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "Read file failed", err.Error())
		return nil, "", false
	}
	if int64(len(data)) > h.maxUploadSize {
		httputil.TooLarge(w, h.maxUploadSize)
		return nil, "", false
	}
	return data, header.Filename, true
}

func (h *Handler) metadata(avatar domain.Avatar) metadataResponse {
	thumbnails := make([]domain.Thumbnail, 0, len(avatar.ThumbnailS3Keys))
	for _, spec := range domain.DefaultThumbnails {
		if avatar.ThumbnailS3Keys[spec.Name] == "" {
			continue
		}
		thumbnails = append(thumbnails, domain.Thumbnail{
			Size: spec.Name,
			URL:  fmt.Sprintf("/api/v1/avatars/%s?size=%s", avatar.ID, spec.Name),
		})
	}
	return metadataResponse{
		ID:       avatar.ID,
		UserID:   avatar.UserID,
		FileName: avatar.FileName,
		MimeType: avatar.MimeType,
		Size:     avatar.SizeBytes,
		Dimensions: domain.Dimensions{
			Width:  avatar.Width,
			Height: avatar.Height,
		},
		Thumbnails: thumbnails,
		CreatedAt:  avatar.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  avatar.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		httputil.Error(w, http.StatusNotFound, "Avatar not found")
	case errors.Is(err, domain.ErrNotReady):
		httputil.Error(w, http.StatusNotFound, "Thumbnail not ready")
	case errors.Is(err, domain.ErrForbidden):
		httputil.Error(w, http.StatusForbidden, "Forbidden", "You can only delete your own avatars")
	case services.IsInvalidInput(err):
		if strings.Contains(err.Error(), "file too large") {
			httputil.TooLarge(w, h.maxUploadSize)
			return
		}
		if strings.Contains(err.Error(), "invalid file format") {
			httputil.Error(w, http.StatusBadRequest, "Invalid file format", imageutil.SupportedFormats())
			return
		}
		httputil.Error(w, http.StatusBadRequest, "Invalid request", err.Error())
	default:
		httputil.Error(w, http.StatusInternalServerError, "Internal server error")
	}
}

func writeImage(w http.ResponseWriter, obj services.ImageObject) {
	if obj.ContentType == "" {
		obj.ContentType = http.DetectContentType(obj.Data)
	}
	w.Header().Set("Content-Type", obj.ContentType)
	w.Header().Set("Cache-Control", "max-age=86400")
	if obj.ETag != "" {
		w.Header().Set("ETag", `"`+strings.Trim(obj.ETag, `"`)+`"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(obj.Data)
}

func validFormat(format string) bool {
	return imageutil.NormalizeFormat(format) != "unsupported"
}
