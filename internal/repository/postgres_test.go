package repository

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestCreateScansTimestamps(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	db := &fakeDB{row: fakeRow{values: []any{now, now}}}
	repo := NewAvatarRepository(db)
	avatar := &domain.Avatar{
		ID:               "avatar-id",
		UserID:           "user-123",
		FileName:         "avatar.png",
		MimeType:         "image/png",
		SizeBytes:        123,
		S3Key:            "key",
		ThumbnailS3Keys:  map[string]string{"100x100": "thumb"},
		UploadStatus:     domain.UploadStatusUploading,
		ProcessingStatus: domain.ProcessingStatusPending,
	}

	if err := repo.Create(context.Background(), avatar); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !avatar.CreatedAt.Equal(now) || !avatar.UpdatedAt.Equal(now) {
		t.Fatalf("timestamps = %v %v", avatar.CreatedAt, avatar.UpdatedAt)
	}
	if db.queryRowCalls != 1 {
		t.Fatalf("QueryRow calls = %d", db.queryRowCalls)
	}
}

func TestGetByIDScansAvatarAndMapsNotFound(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	thumbs, _ := json.Marshal(map[string]string{"100x100": "thumb"})
	db := &fakeDB{row: avatarRow(now, thumbs)}
	repo := NewAvatarRepository(db)

	avatar, err := repo.GetByID(context.Background(), "avatar-id")
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if avatar.ID != "avatar-id" || avatar.ThumbnailS3Keys["100x100"] != "thumb" {
		t.Fatalf("avatar = %+v", avatar)
	}

	db.row = fakeRow{err: pgx.ErrNoRows}
	_, err = repo.GetByID(context.Background(), "missing")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetLatestByUserIDScansAvatar(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	db := &fakeDB{row: avatarRow(now, []byte(`{}`))}
	repo := NewAvatarRepository(db)

	avatar, err := repo.GetLatestByUserID(context.Background(), "user-123")
	if err != nil {
		t.Fatalf("GetLatestByUserID() error = %v", err)
	}
	if avatar.UserID != "user-123" {
		t.Fatalf("avatar = %+v", avatar)
	}
}

func TestListByUserIDScansRows(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	db := &fakeDB{rows: &fakeRows{rows: [][]any{
		avatarRow(now, []byte(`{}`)).values,
	}}}
	repo := NewAvatarRepository(db)

	avatars, err := repo.ListByUserID(context.Background(), "user-123")
	if err != nil {
		t.Fatalf("ListByUserID() error = %v", err)
	}
	if len(avatars) != 1 || avatars[0].ID != "avatar-id" {
		t.Fatalf("avatars = %+v", avatars)
	}
}

func TestRecoveryListsScanRows(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	db := &fakeDB{rows: &fakeRows{rows: [][]any{
		avatarRow(now, []byte(`{}`)).values,
	}}}
	repo := NewAvatarRepository(db)

	avatars, err := repo.ListPendingProcessing(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListPendingProcessing() error = %v", err)
	}
	if len(avatars) != 1 {
		t.Fatalf("pending processing = %+v", avatars)
	}

	db.rows = &fakeRows{rows: [][]any{avatarRow(now, []byte(`{}`)).values}}
	avatars, err = repo.ListPendingCleanup(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListPendingCleanup() error = %v", err)
	}
	if len(avatars) != 1 {
		t.Fatalf("pending cleanup = %+v", avatars)
	}
}

func TestExecMethodsReturnNotFoundWhenNoRowsAffected(t *testing.T) {
	db := &fakeDB{tag: pgconn.NewCommandTag("UPDATE 0")}
	repo := NewAvatarRepository(db)

	err := repo.UpdateUploadStatus(context.Background(), "avatar-id", domain.UploadStatusUploaded)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	db.tag = pgconn.NewCommandTag("UPDATE 1")
	if err := repo.SoftDelete(context.Background(), "avatar-id"); err != nil {
		t.Fatalf("SoftDelete() error = %v", err)
	}
	if err := repo.UpdateProcessingStatus(context.Background(), "avatar-id", domain.ProcessingStatusCompleted); err != nil {
		t.Fatalf("UpdateProcessingStatus() error = %v", err)
	}
	if err := repo.UpdateThumbnailsAndStatus(context.Background(), "avatar-id", map[string]string{"100x100": "thumb"}, domain.ProcessingStatusCompleted); err != nil {
		t.Fatalf("UpdateThumbnailsAndStatus() error = %v", err)
	}
	if err := repo.UpdateCleanupStatus(context.Background(), "avatar-id", domain.CleanupStatusCompleted); err != nil {
		t.Fatalf("UpdateCleanupStatus() error = %v", err)
	}
	if err := repo.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
}

func avatarRow(now time.Time, thumbs []byte) fakeRow {
	return fakeRow{values: []any{
		"avatar-id",
		"user-123",
		"avatar.png",
		"image/png",
		int64(123),
		"key",
		thumbs,
		10,
		20,
		domain.UploadStatusUploaded,
		domain.ProcessingStatusCompleted,
		domain.CleanupStatusPending,
		now,
		now,
		(*time.Time)(nil),
	}}
}

type fakeDB struct {
	row           fakeRow
	rows          pgx.Rows
	tag           pgconn.CommandTag
	err           error
	queryRowCalls int
}

func (db *fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return db.rows, db.err
}

func (db *fakeDB) QueryRow(context.Context, string, ...any) pgx.Row {
	db.queryRowCalls++
	return db.row
}

func (db *fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return db.tag, db.err
}

func (db *fakeDB) Ping(context.Context) error {
	return db.err
}

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		assign(dest[i], r.values[i])
	}
	return nil
}

type fakeRows struct {
	rows   [][]any
	idx    int
	closed bool
	err    error
}

func (r *fakeRows) Close() {
	r.closed = true
}

func (r *fakeRows) Err() error {
	return r.err
}

func (r *fakeRows) CommandTag() pgconn.CommandTag {
	return pgconn.NewCommandTag("SELECT 1")
}

func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *fakeRows) Next() bool {
	if r.idx >= len(r.rows) {
		r.closed = true
		return false
	}
	r.idx++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	values := r.rows[r.idx-1]
	for i := range dest {
		assign(dest[i], values[i])
	}
	return nil
}

func (r *fakeRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, nil
	}
	return r.rows[r.idx-1], nil
}

func (r *fakeRows) RawValues() [][]byte {
	return nil
}

func (r *fakeRows) Conn() *pgx.Conn {
	return nil
}

func assign(dest any, value any) {
	d := reflect.ValueOf(dest).Elem()
	if value == nil {
		d.Set(reflect.Zero(d.Type()))
		return
	}
	v := reflect.ValueOf(value)
	if v.Type().AssignableTo(d.Type()) {
		d.Set(v)
		return
	}
	if v.Type().ConvertibleTo(d.Type()) {
		d.Set(v.Convert(d.Type()))
		return
	}
	panic("cannot assign " + v.Type().String() + " to " + d.Type().String())
}
