package worker

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
)

const testAvatarID = "11111111-1111-4111-8111-111111111111"
const secondTestAvatarID = "22222222-2222-4222-8222-222222222222"

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

func TestRecoverPendingUploadsContinuesAfterItemError(t *testing.T) {
	badAvatar := domain.Avatar{
		ID:               testAvatarID,
		UserID:           "user-123",
		S3Key:            "avatars/" + testAvatarID + "/original.png",
		ProcessingStatus: domain.ProcessingStatusPending,
	}
	goodAvatar := domain.Avatar{
		ID:               secondTestAvatarID,
		UserID:           "user-123",
		S3Key:            "avatars/" + secondTestAvatarID + "/original.png",
		ProcessingStatus: domain.ProcessingStatusPending,
	}
	repo := &recoveryRepo{
		avatars: map[string]*domain.Avatar{
			badAvatar.ID:  &badAvatar,
			goodAvatar.ID: &goodAvatar,
		},
		pendingProcessing: []domain.Avatar{badAvatar, goodAvatar},
	}
	store := &workerStorage{objects: map[string]storage.Object{
		goodAvatar.S3Key: {Key: goodAvatar.S3Key, Data: workerPNG(t), ContentType: "image/png"},
	}}
	processor := NewProcessor(repo, store, nil)

	count, err := processor.RecoverPendingUploads(context.Background(), 10)
	if err == nil {
		t.Fatal("expected partial recovery error")
	}
	if count != 1 {
		t.Fatalf("recovery count = %d, want 1", count)
	}
	if repo.avatars[badAvatar.ID].ProcessingStatus != domain.ProcessingStatusFailed {
		t.Fatalf("bad avatar status = %q", repo.avatars[badAvatar.ID].ProcessingStatus)
	}
	if repo.avatars[goodAvatar.ID].ProcessingStatus != domain.ProcessingStatusCompleted {
		t.Fatalf("good avatar status = %q", repo.avatars[goodAvatar.ID].ProcessingStatus)
	}
}

func TestRecoverPendingDeletesContinuesAfterItemError(t *testing.T) {
	badAvatar := domain.Avatar{
		ID:              testAvatarID,
		S3Key:           "bad-key",
		ThumbnailS3Keys: map[string]string{},
		CleanupStatus:   domain.CleanupStatusPending,
	}
	goodAvatar := domain.Avatar{
		ID:              secondTestAvatarID,
		S3Key:           "good-key",
		ThumbnailS3Keys: map[string]string{},
		CleanupStatus:   domain.CleanupStatusPending,
	}
	repo := &recoveryRepo{
		avatars: map[string]*domain.Avatar{
			badAvatar.ID:  &badAvatar,
			goodAvatar.ID: &goodAvatar,
		},
		pendingCleanup: []domain.Avatar{badAvatar, goodAvatar},
	}
	store := &workerStorage{
		objects:       map[string]storage.Object{"bad-key": {Key: "bad-key"}, "good-key": {Key: "good-key"}},
		deleteErrKeys: map[string]error{"bad-key": errors.New("delete failed")},
	}
	processor := NewProcessor(repo, store, nil)

	count, err := processor.RecoverPendingDeletes(context.Background(), 10)
	if err == nil {
		t.Fatal("expected partial recovery error")
	}
	if count != 1 {
		t.Fatalf("recovery count = %d, want 1", count)
	}
	if repo.avatars[badAvatar.ID].CleanupStatus != domain.CleanupStatusFailed {
		t.Fatalf("bad avatar cleanup status = %q", repo.avatars[badAvatar.ID].CleanupStatus)
	}
	if repo.avatars[goodAvatar.ID].CleanupStatus != domain.CleanupStatusCompleted {
		t.Fatalf("good avatar cleanup status = %q", repo.avatars[goodAvatar.ID].CleanupStatus)
	}
	if _, ok := store.objects["good-key"]; ok {
		t.Fatal("good key was not deleted")
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
	objects       map[string]storage.Object
	deleteErrKeys map[string]error
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
		if err := s.deleteErrKeys[key]; err != nil {
			return err
		}
		delete(s.objects, key)
	}
	return nil
}

type recoveryRepo struct {
	avatars           map[string]*domain.Avatar
	pendingProcessing []domain.Avatar
	pendingCleanup    []domain.Avatar
}

func (r *recoveryRepo) GetByID(_ context.Context, id string) (*domain.Avatar, error) {
	avatar := r.avatars[id]
	if avatar == nil {
		return nil, domain.ErrNotFound
	}
	cp := *avatar
	return &cp, nil
}

func (r *recoveryRepo) UpdateProcessingStatus(_ context.Context, id string, status domain.ProcessingStatus) error {
	if avatar := r.avatars[id]; avatar != nil {
		avatar.ProcessingStatus = status
	}
	return nil
}

func (r *recoveryRepo) UpdateThumbnailsAndStatus(_ context.Context, id string, thumbnails map[string]string, status domain.ProcessingStatus) error {
	avatar := r.avatars[id]
	if avatar != nil {
		avatar.ThumbnailS3Keys = thumbnails
		avatar.ProcessingStatus = status
	}
	return nil
}

func (r *recoveryRepo) ListPendingProcessing(context.Context, int) ([]domain.Avatar, error) {
	return append([]domain.Avatar(nil), r.pendingProcessing...), nil
}

func (r *recoveryRepo) ListPendingCleanup(context.Context, int) ([]domain.Avatar, error) {
	return append([]domain.Avatar(nil), r.pendingCleanup...), nil
}

func (r *recoveryRepo) UpdateCleanupStatus(_ context.Context, id string, status domain.CleanupStatus) error {
	if avatar := r.avatars[id]; avatar != nil {
		avatar.CleanupStatus = status
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
