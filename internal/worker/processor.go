package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/pkg/imageutil"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
)

type Repository interface {
	GetByID(ctx context.Context, id string) (*domain.Avatar, error)
	ListPendingProcessing(ctx context.Context, limit int) ([]domain.Avatar, error)
	ListPendingCleanup(ctx context.Context, limit int) ([]domain.Avatar, error)
	UpdateProcessingStatus(ctx context.Context, id string, status domain.ProcessingStatus) error
	UpdateThumbnailsAndStatus(ctx context.Context, id string, thumbnails map[string]string, status domain.ProcessingStatus) error
	UpdateCleanupStatus(ctx context.Context, id string, status domain.CleanupStatus) error
}

type ObjectStorage interface {
	Download(ctx context.Context, key string) (storage.Object, error)
	Upload(ctx context.Context, key string, data []byte, contentType string) error
	DeleteMany(ctx context.Context, keys []string) error
}

type Processor struct {
	repo  Repository
	store ObjectStorage
	log   *slog.Logger
}

func NewProcessor(repo Repository, store ObjectStorage, log *slog.Logger) *Processor {
	if log == nil {
		log = slog.Default()
	}
	return &Processor{repo: repo, store: store, log: log}
}

func (p *Processor) HandleUpload(ctx context.Context, event domain.AvatarUploadEvent) error {
	if event.AvatarID == "" || event.S3Key == "" {
		return fmt.Errorf("%w: empty upload event fields", domain.ErrInvalidInput)
	}
	if _, err := uuid.Parse(event.AvatarID); err != nil {
		return fmt.Errorf("%w: invalid avatar id", domain.ErrInvalidInput)
	}

	avatar, err := p.repo.GetByID(ctx, event.AvatarID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	if avatar.ProcessingStatus == domain.ProcessingStatusCompleted {
		return nil
	}
	if err := p.repo.UpdateProcessingStatus(ctx, avatar.ID, domain.ProcessingStatusProcessing); err != nil {
		return err
	}

	original, err := p.store.Download(ctx, event.S3Key)
	if err != nil {
		_ = p.repo.UpdateProcessingStatus(ctx, avatar.ID, domain.ProcessingStatusFailed)
		return err
	}

	thumbnails := make(map[string]string, len(domain.DefaultThumbnails))
	for _, spec := range domain.DefaultThumbnails {
		data, err := imageutil.ResizeJPEG(original.Data, spec.Width, spec.Height)
		if err != nil {
			_ = p.repo.UpdateProcessingStatus(ctx, avatar.ID, domain.ProcessingStatusFailed)
			return err
		}
		key := fmt.Sprintf("thumbnails/%s/%s.jpg", avatar.ID, spec.Name)
		if err := p.store.Upload(ctx, key, data, imageutil.MimeJPEG); err != nil {
			_ = p.repo.UpdateProcessingStatus(ctx, avatar.ID, domain.ProcessingStatusFailed)
			return err
		}
		thumbnails[spec.Name] = key
	}

	if err := p.repo.UpdateThumbnailsAndStatus(ctx, avatar.ID, thumbnails, domain.ProcessingStatusCompleted); err != nil {
		_ = p.repo.UpdateProcessingStatus(ctx, avatar.ID, domain.ProcessingStatusFailed)
		_ = p.store.DeleteMany(ctx, thumbnailKeys(thumbnails))
		return err
	}
	return nil
}

func (p *Processor) HandleDelete(ctx context.Context, event domain.AvatarDeleteEvent) error {
	if event.AvatarID == "" {
		return fmt.Errorf("%w: empty avatar id", domain.ErrInvalidInput)
	}
	if _, err := uuid.Parse(event.AvatarID); err != nil {
		return fmt.Errorf("%w: invalid avatar id", domain.ErrInvalidInput)
	}
	if err := p.store.DeleteMany(ctx, event.S3Keys); err != nil {
		_ = p.repo.UpdateCleanupStatus(ctx, event.AvatarID, domain.CleanupStatusFailed)
		return err
	}
	return p.repo.UpdateCleanupStatus(ctx, event.AvatarID, domain.CleanupStatusCompleted)
}

func (p *Processor) RecoverPendingUploads(ctx context.Context, limit int) (int, error) {
	avatars, err := p.repo.ListPendingProcessing(ctx, limit)
	if err != nil {
		return 0, err
	}
	var errs []error
	processed := 0
	for _, avatar := range avatars {
		if err := p.HandleUpload(ctx, domain.AvatarUploadEvent{
			AvatarID: avatar.ID,
			UserID:   avatar.UserID,
			S3Key:    avatar.S3Key,
		}); err != nil {
			p.log.Warn("pending upload recovery item failed", "avatar_id", avatar.ID, "err", err)
			errs = append(errs, err)
			continue
		}
		processed++
	}
	return processed, errors.Join(errs...)
}

func (p *Processor) RecoverPendingDeletes(ctx context.Context, limit int) (int, error) {
	avatars, err := p.repo.ListPendingCleanup(ctx, limit)
	if err != nil {
		return 0, err
	}
	var errs []error
	processed := 0
	for _, avatar := range avatars {
		if err := p.HandleDelete(ctx, domain.AvatarDeleteEvent{
			AvatarID: avatar.ID,
			S3Keys:   avatar.S3Keys(),
		}); err != nil {
			p.log.Warn("pending delete recovery item failed", "avatar_id", avatar.ID, "err", err)
			errs = append(errs, err)
			continue
		}
		processed++
	}
	return processed, errors.Join(errs...)
}

func thumbnailKeys(thumbnails map[string]string) []string {
	keys := make([]string, 0, len(thumbnails))
	for _, key := range thumbnails {
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}
