package worker

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
)

const testAvatarID = "11111111-1111-4111-8111-111111111111"

func TestHandleUploadCreatesThumbnailsAndMarksCompleted(t *testing.T) {
	repo := &workerRepo{avatar: &domain.Avatar{
		ID:               testAvatarID,
		UserID:           "user-123",
		S3Key:            "avatars/" + testAvatarID + "/original.png",
		ProcessingStatus: domain.ProcessingStatusPending,
	}}
	store := &workerStorage{objects: map[string]storage.Object{
		"avatars/" + testAvatarID + "/original.png": {Key: "avatars/" + testAvatarID + "/original.png", Data: workerPNG(t), ContentType: "image/png"},
	}}
	processor := NewProcessor(repo, store, nil)

	err := processor.HandleUpload(context.Background(), domain.AvatarUploadEvent{
		AvatarID: testAvatarID,
		UserID:   "user-123",
		S3Key:    "avatars/" + testAvatarID + "/original.png",
	})
	if err != nil {
		t.Fatalf("HandleUpload() error = %v", err)
	}

	if repo.avatar.ProcessingStatus != domain.ProcessingStatusCompleted {
		t.Fatalf("processing status = %q", repo.avatar.ProcessingStatus)
	}
	if len(repo.avatar.ThumbnailS3Keys) != 2 {
		t.Fatalf("thumbnail keys = %+v", repo.avatar.ThumbnailS3Keys)
	}
	for _, key := range repo.avatar.ThumbnailS3Keys {
		if _, ok := store.objects[key]; !ok {
			t.Fatalf("missing thumbnail object %q", key)
		}
	}
}

func TestHandleUploadSkipsCompletedAvatar(t *testing.T) {
	repo := &workerRepo{avatar: &domain.Avatar{
		ID:               testAvatarID,
		S3Key:            "avatars/" + testAvatarID + "/original.png",
		ProcessingStatus: domain.ProcessingStatusCompleted,
	}}
	store := &workerStorage{objects: map[string]storage.Object{}}
	processor := NewProcessor(repo, store, nil)

	if err := processor.HandleUpload(context.Background(), domain.AvatarUploadEvent{AvatarID: testAvatarID, S3Key: "avatars/" + testAvatarID + "/original.png"}); err != nil {
		t.Fatalf("HandleUpload() error = %v", err)
	}
	if len(store.objects) != 0 {
		t.Fatalf("unexpected storage writes: %+v", store.objects)
	}
}

func TestHandleDeleteRemovesAllKeys(t *testing.T) {
	store := &workerStorage{objects: map[string]storage.Object{
		"one": {Key: "one", Data: []byte("1")},
		"two": {Key: "two", Data: []byte("2")},
	}}
	repo := &workerRepo{avatar: &domain.Avatar{ID: testAvatarID}}
	processor := NewProcessor(repo, store, nil)

	if err := processor.HandleDelete(context.Background(), domain.AvatarDeleteEvent{AvatarID: testAvatarID, S3Keys: []string{"one", "two"}}); err != nil {
		t.Fatalf("HandleDelete() error = %v", err)
	}
	if len(store.objects) != 0 {
		t.Fatalf("objects were not deleted: %+v", store.objects)
	}
	if repo.avatar.CleanupStatus != domain.CleanupStatusCompleted {
		t.Fatalf("cleanup status = %q", repo.avatar.CleanupStatus)
	}
}

func TestRecoverPendingUploadsAndDeletes(t *testing.T) {
	repo := &workerRepo{avatar: &domain.Avatar{
		ID:               testAvatarID,
		UserID:           "user-123",
		S3Key:            "avatars/" + testAvatarID + "/original.png",
		ProcessingStatus: domain.ProcessingStatusPending,
		ThumbnailS3Keys:  map[string]string{"100x100": "one"},
	}}
	store := &workerStorage{objects: map[string]storage.Object{
		"avatars/" + testAvatarID + "/original.png": {Key: "avatars/" + testAvatarID + "/original.png", Data: workerPNG(t), ContentType: "image/png"},
		"one": {Key: "one", Data: []byte("1")},
	}}
	processor := NewProcessor(repo, store, nil)

	count, err := processor.RecoverPendingUploads(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecoverPendingUploads() error = %v", err)
	}
	if count != 1 || repo.avatar.ProcessingStatus != domain.ProcessingStatusCompleted {
		t.Fatalf("upload recovery count/status = %d/%q", count, repo.avatar.ProcessingStatus)
	}

	count, err = processor.RecoverPendingDeletes(context.Background(), 10)
	if err != nil {
		t.Fatalf("RecoverPendingDeletes() error = %v", err)
	}
	if count != 1 || repo.avatar.CleanupStatus != domain.CleanupStatusCompleted {
		t.Fatalf("delete recovery count/status = %d/%q", count, repo.avatar.CleanupStatus)
	}
}

type workerRepo struct {
	avatar *domain.Avatar
}

func (r *workerRepo) GetByID(context.Context, string) (*domain.Avatar, error) {
	if r.avatar == nil {
		return nil, domain.ErrNotFound
	}
	cp := *r.avatar
	return &cp, nil
}

func (r *workerRepo) UpdateProcessingStatus(_ context.Context, _ string, status domain.ProcessingStatus) error {
	if r.avatar != nil {
		r.avatar.ProcessingStatus = status
	}
	return nil
}

func (r *workerRepo) UpdateThumbnailsAndStatus(_ context.Context, _ string, thumbnails map[string]string, status domain.ProcessingStatus) error {
	r.avatar.ThumbnailS3Keys = thumbnails
	r.avatar.ProcessingStatus = status
	return nil
}

func (r *workerRepo) ListPendingProcessing(context.Context, int) ([]domain.Avatar, error) {
	if r.avatar == nil {
		return nil, nil
	}
	return []domain.Avatar{*r.avatar}, nil
}

func (r *workerRepo) ListPendingCleanup(context.Context, int) ([]domain.Avatar, error) {
	if r.avatar == nil {
		return nil, nil
	}
	return []domain.Avatar{*r.avatar}, nil
}

func (r *workerRepo) UpdateCleanupStatus(_ context.Context, _ string, status domain.CleanupStatus) error {
	if r.avatar != nil {
		r.avatar.CleanupStatus = status
	}
	return nil
}

type workerStorage struct {
	objects map[string]storage.Object
}

func (s *workerStorage) Download(_ context.Context, key string) (storage.Object, error) {
	obj, ok := s.objects[key]
	if !ok {
		return storage.Object{}, domain.ErrNotFound
	}
	return obj, nil
}

func (s *workerStorage) Upload(_ context.Context, key string, data []byte, contentType string) error {
	s.objects[key] = storage.Object{Key: key, Data: append([]byte(nil), data...), ContentType: contentType}
	return nil
}

func (s *workerStorage) DeleteMany(_ context.Context, keys []string) error {
	for _, key := range keys {
		delete(s.objects, key)
	}
	return nil
}

func workerPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 50, G: 80, B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
