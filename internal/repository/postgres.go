package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/internal/observability"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Ping(ctx context.Context) error
}

type AvatarRepository struct {
	db DB
}

func NewAvatarRepository(db DB) *AvatarRepository {
	return &AvatarRepository{db: db}
}

func Connect(ctx context.Context, dsn string) (pool *pgxpool.Pool, err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.connect")
	defer func() {
		observability.EndSpan(span, err)
	}()

	pool, err = pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func (r *AvatarRepository) Create(ctx context.Context, a *domain.Avatar) (err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.create")
	defer func() {
		span.SetAttributes(
			attribute.String("avatar_id", a.ID),
			attribute.String("user_id", a.UserID),
		)
		observability.EndSpan(span, err)
	}()

	var thumbs []byte
	if a.ThumbnailS3Keys != nil {
		thumbs, err = json.Marshal(a.ThumbnailS3Keys)
		if err != nil {
			return fmt.Errorf("marshal thumbnails: %w", err)
		}
	}

	return r.db.QueryRow(ctx, `
		INSERT INTO avatars (
			id, user_id, file_name, mime_type, size_bytes, s3_key, thumbnail_s3_keys,
			width, height, upload_status, processing_status
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING created_at, updated_at
	`,
		a.ID, a.UserID, a.FileName, a.MimeType, a.SizeBytes, a.S3Key, thumbs,
		a.Width, a.Height, a.UploadStatus, a.ProcessingStatus,
	).Scan(&a.CreatedAt, &a.UpdatedAt)
}

func (r *AvatarRepository) GetByID(ctx context.Context, id string) (avatar *domain.Avatar, err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.get_by_id")
	defer func() {
		span.SetAttributes(attribute.String("avatar_id", id))
		observability.EndSpan(span, err)
	}()

	return r.scanOne(ctx, `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		       thumbnail_s3_keys, width, height, upload_status, processing_status,
		       cleanup_status, created_at, updated_at, deleted_at
		FROM avatars
		WHERE id = $1 AND deleted_at IS NULL
	`, id)
}

func (r *AvatarRepository) GetLatestByUserID(ctx context.Context, userID string) (avatar *domain.Avatar, err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.get_latest_by_user")
	defer func() {
		span.SetAttributes(attribute.String("user_id", userID))
		observability.EndSpan(span, err)
	}()

	return r.scanOne(ctx, `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		       thumbnail_s3_keys, width, height, upload_status, processing_status,
		       cleanup_status, created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, userID)
}

func (r *AvatarRepository) ListByUserID(ctx context.Context, userID string) (avatars []domain.Avatar, err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.list_by_user")
	defer func() {
		span.SetAttributes(attribute.String("user_id", userID))
		observability.EndSpan(span, err)
	}()

	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		       thumbnail_s3_keys, width, height, upload_status, processing_status,
		       cleanup_status, created_at, updated_at, deleted_at
		FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	avatars = make([]domain.Avatar, 0)
	for rows.Next() {
		avatar, err := scanAvatar(rows)
		if err != nil {
			return nil, err
		}
		avatars = append(avatars, *avatar)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return avatars, nil
}

func (r *AvatarRepository) ListPendingProcessing(ctx context.Context, limit int) (avatars []domain.Avatar, err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.list_pending_processing")
	defer func() {
		span.SetAttributes(attribute.Int("limit", limit))
		observability.EndSpan(span, err)
	}()

	return r.list(ctx, `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		       thumbnail_s3_keys, width, height, upload_status, processing_status,
		       cleanup_status, created_at, updated_at, deleted_at
		FROM avatars
		WHERE deleted_at IS NULL
		  AND upload_status = $1
		  AND processing_status IN ($2, $3)
		ORDER BY updated_at ASC
		LIMIT $4
	`, domain.UploadStatusUploaded, domain.ProcessingStatusPending, domain.ProcessingStatusFailed, limit)
}

func (r *AvatarRepository) ListPendingCleanup(ctx context.Context, limit int) (avatars []domain.Avatar, err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.list_pending_cleanup")
	defer func() {
		span.SetAttributes(attribute.Int("limit", limit))
		observability.EndSpan(span, err)
	}()

	return r.list(ctx, `
		SELECT id, user_id, file_name, mime_type, size_bytes, s3_key,
		       thumbnail_s3_keys, width, height, upload_status, processing_status,
		       cleanup_status, created_at, updated_at, deleted_at
		FROM avatars
		WHERE deleted_at IS NOT NULL
		  AND cleanup_status IN ($1, $2)
		ORDER BY updated_at ASC
		LIMIT $3
	`, domain.CleanupStatusPending, domain.CleanupStatusFailed, limit)
}

func (r *AvatarRepository) UpdateUploadStatus(ctx context.Context, id string, status domain.UploadStatus) (err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.update_upload_status")
	defer func() {
		span.SetAttributes(attribute.String("avatar_id", id), attribute.String("status", string(status)))
		observability.EndSpan(span, err)
	}()

	return r.execOne(ctx, `
		UPDATE avatars
		SET upload_status = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, id, status)
}

func (r *AvatarRepository) UpdateProcessingStatus(ctx context.Context, id string, status domain.ProcessingStatus) (err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.update_processing_status")
	defer func() {
		span.SetAttributes(attribute.String("avatar_id", id), attribute.String("status", string(status)))
		observability.EndSpan(span, err)
	}()

	return r.execOne(ctx, `
		UPDATE avatars
		SET processing_status = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, id, status)
}

func (r *AvatarRepository) UpdateThumbnailsAndStatus(ctx context.Context, id string, thumbnails map[string]string, status domain.ProcessingStatus) (err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.update_thumbnails")
	defer func() {
		span.SetAttributes(attribute.String("avatar_id", id), attribute.String("status", string(status)))
		observability.EndSpan(span, err)
	}()

	data, err := json.Marshal(thumbnails)
	if err != nil {
		return fmt.Errorf("marshal thumbnails: %w", err)
	}
	return r.execOne(ctx, `
		UPDATE avatars
		SET thumbnail_s3_keys = $2, processing_status = $3, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, id, data, status)
}

func (r *AvatarRepository) SoftDelete(ctx context.Context, id string) (err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.soft_delete")
	defer func() {
		span.SetAttributes(attribute.String("avatar_id", id))
		observability.EndSpan(span, err)
	}()

	return r.execOne(ctx, `
		UPDATE avatars
		SET deleted_at = NOW(), cleanup_status = $2, updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`, id, domain.CleanupStatusPending)
}

func (r *AvatarRepository) UpdateCleanupStatus(ctx context.Context, id string, status domain.CleanupStatus) (err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.avatar.update_cleanup_status")
	defer func() {
		span.SetAttributes(attribute.String("avatar_id", id), attribute.String("status", string(status)))
		observability.EndSpan(span, err)
	}()

	return r.execOne(ctx, `
		UPDATE avatars
		SET cleanup_status = $2, updated_at = NOW()
		WHERE id = $1
	`, id, status)
}

func (r *AvatarRepository) Ping(ctx context.Context) (err error) {
	ctx, span := otel.Tracer(observability.TracerName).Start(ctx, "db.ping")
	defer func() {
		observability.EndSpan(span, err)
	}()

	return r.db.Ping(ctx)
}

func (r *AvatarRepository) scanOne(ctx context.Context, query string, args ...any) (*domain.Avatar, error) {
	row := r.db.QueryRow(ctx, query, args...)
	avatar, err := scanAvatar(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return avatar, nil
}

func (r *AvatarRepository) execOne(ctx context.Context, query string, args ...any) error {
	tag, err := r.db.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *AvatarRepository) list(ctx context.Context, query string, args ...any) ([]domain.Avatar, error) {
	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	avatars := make([]domain.Avatar, 0)
	for rows.Next() {
		avatar, err := scanAvatar(rows)
		if err != nil {
			return nil, err
		}
		avatars = append(avatars, *avatar)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return avatars, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAvatar(row rowScanner) (*domain.Avatar, error) {
	var avatar domain.Avatar
	var thumbnails []byte
	if err := row.Scan(
		&avatar.ID,
		&avatar.UserID,
		&avatar.FileName,
		&avatar.MimeType,
		&avatar.SizeBytes,
		&avatar.S3Key,
		&thumbnails,
		&avatar.Width,
		&avatar.Height,
		&avatar.UploadStatus,
		&avatar.ProcessingStatus,
		&avatar.CleanupStatus,
		&avatar.CreatedAt,
		&avatar.UpdatedAt,
		&avatar.DeletedAt,
	); err != nil {
		return nil, err
	}
	avatar.ThumbnailS3Keys = map[string]string{}
	if len(thumbnails) > 0 {
		if err := json.Unmarshal(thumbnails, &avatar.ThumbnailS3Keys); err != nil {
			return nil, fmt.Errorf("unmarshal thumbnails: %w", err)
		}
	}
	return &avatar, nil
}
