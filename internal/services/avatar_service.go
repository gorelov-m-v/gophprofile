package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/pkg/imageutil"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
)

var validUserID = regexp.MustCompile(`^[A-Za-z0-9._@:-]{1,255}$`)

type AvatarRepository interface {
	Create(ctx context.Context, a *domain.Avatar) error
	GetByID(ctx context.Context, id string) (*domain.Avatar, error)
	GetLatestByUserID(ctx context.Context, userID string) (*domain.Avatar, error)
	ListByUserID(ctx context.Context, userID string) ([]domain.Avatar, error)
	UpdateUploadStatus(ctx context.Context, id string, status domain.UploadStatus) error
	UpdateProcessingStatus(ctx context.Context, id string, status domain.ProcessingStatus) error
	UpdateThumbnailsAndStatus(ctx context.Context, id string, thumbnails map[string]string, status domain.ProcessingStatus) error
	SoftDelete(ctx context.Context, id string) error
	Ping(ctx context.Context) error
}

type ObjectStorage interface {
	EnsureBucket(ctx context.Context) error
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	Download(ctx context.Context, key string) (storage.Object, error)
	Delete(ctx context.Context, key string) error
	DeleteMany(ctx context.Context, keys []string) error
	Ping(ctx context.Context) error
	PublicURL(key string) string
}

type EventPublisher interface {
	PublishUpload(ctx context.Context, event domain.AvatarUploadEvent) error
	PublishDelete(ctx context.Context, event domain.AvatarDeleteEvent) error
	Ping() error
}

type AvatarService struct {
	repo          AvatarRepository
	store         ObjectStorage
	publisher     EventPublisher
	maxUploadSize int64
}

func NewAvatarService(repo AvatarRepository, store ObjectStorage, publisher EventPublisher, maxUploadSize int64) *AvatarService {
	return &AvatarService{
		repo:          repo,
		store:         store,
		publisher:     publisher,
		maxUploadSize: maxUploadSize,
	}
}

type UploadInput struct {
	UserID   string
	FileName string
	Data     []byte
}

type ImageObject struct {
	Data        []byte
	ContentType string
	ETag        string
}

func (s *AvatarService) Upload(ctx context.Context, input UploadInput) (*domain.Avatar, error) {
	if err := ValidateUserID(input.UserID); err != nil {
		return nil, err
	}
	if len(input.Data) == 0 {
		return nil, fmt.Errorf("%w: file is required", domain.ErrInvalidInput)
	}

	mime, err := imageutil.Validate(input.Data, s.maxUploadSize)
	if err != nil {
		if strings.Contains(err.Error(), "too large") {
			return nil, fmt.Errorf("%w: file too large", domain.ErrInvalidInput)
		}
		return nil, fmt.Errorf("%w: invalid file format", domain.ErrInvalidInput)
	}
	width, height, err := imageutil.Dimensions(input.Data)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid image", domain.ErrInvalidInput)
	}

	id := uuid.NewString()
	fileName := cleanFileName(input.FileName, mime)
	key := fmt.Sprintf("avatars/%s/original%s", id, imageutil.ExtensionForMime(mime))

	avatar := &domain.Avatar{
		ID:               id,
		UserID:           input.UserID,
		FileName:         fileName,
		MimeType:         mime,
		SizeBytes:        int64(len(input.Data)),
		S3Key:            key,
		ThumbnailS3Keys:  map[string]string{},
		Width:            width,
		Height:           height,
		UploadStatus:     domain.UploadStatusUploading,
		ProcessingStatus: domain.ProcessingStatusPending,
	}
	if err := s.repo.Create(ctx, avatar); err != nil {
		return nil, fmt.Errorf("create avatar metadata: %w", err)
	}
	if err := s.store.Upload(ctx, key, input.Data, mime); err != nil {
		_ = s.repo.UpdateUploadStatus(ctx, id, domain.UploadStatusFailed)
		_ = s.repo.UpdateProcessingStatus(ctx, id, domain.ProcessingStatusFailed)
		return nil, fmt.Errorf("upload original: %w", err)
	}
	avatar.UploadStatus = domain.UploadStatusUploaded
	if err := s.repo.UpdateUploadStatus(ctx, id, domain.UploadStatusUploaded); err != nil {
		_ = s.store.Delete(ctx, key)
		return nil, fmt.Errorf("mark uploaded: %w", err)
	}
	if err := s.publisher.PublishUpload(ctx, domain.AvatarUploadEvent{
		MessageID: uuid.NewString(),
		AvatarID:  id,
		UserID:    input.UserID,
		S3Key:     key,
	}); err != nil {
		return avatar, nil
	}
	return avatar, nil
}

func (s *AvatarService) GetImage(ctx context.Context, avatarID, size, format string) (ImageObject, error) {
	if err := ValidateAvatarID(avatarID); err != nil {
		return ImageObject{}, err
	}
	avatar, err := s.repo.GetByID(ctx, avatarID)
	if err != nil {
		return ImageObject{}, err
	}
	return s.objectForAvatar(ctx, avatar, size, format)
}

func (s *AvatarService) GetLatestUserImage(ctx context.Context, userID, size, format string) (ImageObject, error) {
	if err := ValidateUserID(userID); err != nil {
		return ImageObject{}, err
	}
	avatar, err := s.repo.GetLatestByUserID(ctx, userID)
	if err != nil {
		return ImageObject{}, err
	}
	return s.objectForAvatar(ctx, avatar, size, format)
}

func (s *AvatarService) objectForAvatar(ctx context.Context, avatar *domain.Avatar, size, format string) (ImageObject, error) {
	key, contentType, err := selectKey(avatar, size)
	if err != nil {
		return ImageObject{}, err
	}
	obj, err := s.store.Download(ctx, key)
	if err != nil {
		return ImageObject{}, err
	}
	if obj.ContentType == "" {
		obj.ContentType = contentType
	}
	converted := false
	if format = imageutil.NormalizeFormat(format); format == "unsupported" {
		return ImageObject{}, fmt.Errorf("%w: unsupported format", domain.ErrInvalidInput)
	}
	if format != "" && imageutil.MimeForFormat(format) != obj.ContentType {
		data, mime, err := imageutil.Convert(obj.Data, format)
		if err != nil {
			return ImageObject{}, fmt.Errorf("%w: %v", domain.ErrInvalidInput, err)
		}
		obj.Data = data
		obj.ContentType = mime
		converted = true
	}
	etag := obj.ETag
	if etag == "" || converted {
		sum := sha256.Sum256(obj.Data)
		etag = hex.EncodeToString(sum[:])
	}
	return ImageObject{Data: obj.Data, ContentType: obj.ContentType, ETag: etag}, nil
}

func (s *AvatarService) GetMetadata(ctx context.Context, avatarID string) (*domain.Avatar, error) {
	if err := ValidateAvatarID(avatarID); err != nil {
		return nil, err
	}
	return s.repo.GetByID(ctx, avatarID)
}

func (s *AvatarService) ListUserAvatars(ctx context.Context, userID string) ([]domain.Avatar, error) {
	if err := ValidateUserID(userID); err != nil {
		return nil, err
	}
	return s.repo.ListByUserID(ctx, userID)
}

func (s *AvatarService) Delete(ctx context.Context, avatarID, requestUserID string) error {
	if err := ValidateAvatarID(avatarID); err != nil {
		return err
	}
	if err := ValidateUserID(requestUserID); err != nil {
		return err
	}
	avatar, err := s.repo.GetByID(ctx, avatarID)
	if err != nil {
		return err
	}
	if avatar.UserID != requestUserID {
		return domain.ErrForbidden
	}
	if err := s.repo.SoftDelete(ctx, avatarID); err != nil {
		return err
	}
	_ = s.publisher.PublishDelete(ctx, domain.AvatarDeleteEvent{
		MessageID: uuid.NewString(),
		AvatarID:  avatarID,
		S3Keys:    avatar.S3Keys(),
	})
	return nil
}

func (s *AvatarService) DeleteLatestByUser(ctx context.Context, pathUserID, requestUserID string) error {
	if pathUserID != requestUserID {
		return domain.ErrForbidden
	}
	avatar, err := s.repo.GetLatestByUserID(ctx, pathUserID)
	if err != nil {
		return err
	}
	return s.Delete(ctx, avatar.ID, requestUserID)
}

func (s *AvatarService) Health(ctx context.Context) map[string]string {
	statuses := map[string]string{
		"database": "ok",
		"s3":       "ok",
		"rabbitmq": "ok",
	}
	if err := s.repo.Ping(ctx); err != nil {
		statuses["database"] = err.Error()
	}
	if err := s.store.Ping(ctx); err != nil {
		statuses["s3"] = err.Error()
	}
	if err := s.publisher.Ping(); err != nil {
		statuses["rabbitmq"] = err.Error()
	}
	return statuses
}

func (s *AvatarService) URLForAvatar(id string) string {
	return "/api/v1/avatars/" + id
}

func (s *AvatarService) URLForS3Key(key string) string {
	return s.store.PublicURL(key)
}

func ValidateUserID(userID string) error {
	if !validUserID.MatchString(userID) {
		return fmt.Errorf("%w: invalid user id", domain.ErrInvalidInput)
	}
	return nil
}

func ValidateAvatarID(avatarID string) error {
	if _, err := uuid.Parse(avatarID); err != nil {
		return fmt.Errorf("%w: invalid avatar id", domain.ErrInvalidInput)
	}
	return nil
}

func selectKey(avatar *domain.Avatar, size string) (string, string, error) {
	switch size {
	case "", "original":
		return avatar.S3Key, avatar.MimeType, nil
	case "100x100", "300x300":
		key := avatar.ThumbnailS3Keys[size]
		if key == "" {
			return "", "", domain.ErrNotReady
		}
		return key, imageutil.MimeJPEG, nil
	default:
		return "", "", fmt.Errorf("%w: unsupported size", domain.ErrInvalidInput)
	}
}

func cleanFileName(name, mime string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "." || name == "" {
		return "avatar" + imageutil.ExtensionForMime(mime)
	}
	if filepath.Ext(name) == "" {
		name += imageutil.ExtensionForMime(mime)
	}
	return name
}

func IsInvalidInput(err error) bool {
	return errors.Is(err, domain.ErrInvalidInput)
}
