package services

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gorelov-m-v/gophprofile/internal/domain"
	"github.com/gorelov-m-v/gophprofile/pkg/imageutil"
	"github.com/gorelov-m-v/gophprofile/pkg/storage"
)

func TestUploadCreatesMetadataStoresOriginalAndPublishesEvent(t *testing.T) {
	repo := newMemoryRepo()
	store := newMemoryStorage()
	pub := &memoryPublisher{}
	svc := NewAvatarService(repo, store, pub, 10<<20)

	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "me.png",
		Data:     testPNG(t, 16, 12),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if avatar.ID == "" {
		t.Fatal("Upload() returned empty id")
	}
	if avatar.UserID != "user-123" || avatar.FileName != "me.png" {
		t.Fatalf("unexpected avatar metadata: %+v", avatar)
	}
	if avatar.MimeType != "image/png" || avatar.Width != 16 || avatar.Height != 12 {
		t.Fatalf("unexpected image metadata: %+v", avatar)
	}
	if repo.avatars[avatar.ID].UploadStatus != domain.UploadStatusUploaded {
		t.Fatalf("upload status = %q", repo.avatars[avatar.ID].UploadStatus)
	}
	if _, ok := store.objects[avatar.S3Key]; !ok {
		t.Fatalf("original object %q was not stored", avatar.S3Key)
	}
	if len(pub.uploads) != 1 || pub.uploads[0].AvatarID != avatar.ID {
		t.Fatalf("upload events = %+v", pub.uploads)
	}
}

func TestUploadRejectsInvalidUserAndInvalidFile(t *testing.T) {
	svc := NewAvatarService(newMemoryRepo(), newMemoryStorage(), &memoryPublisher{}, 10)

	_, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "bad user",
		FileName: "avatar.txt",
		Data:     []byte("not an image"),
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid input for user id, got %v", err)
	}

	_, err = svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "avatar.txt",
		Data:     []byte("not an image"),
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid input for file, got %v", err)
	}

	_, err = svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "huge.png",
		Data:     testPNG(t, imageutil.MaxImageWidth+1, 1),
	})
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid input for image dimensions, got %v", err)
	}
}

func TestUploadStillReturnsAvatarWhenPublishFails(t *testing.T) {
	repo := newMemoryRepo()
	pub := &memoryPublisher{err: errors.New("rabbit down")}
	svc := NewAvatarService(repo, newMemoryStorage(), pub, 10<<20)

	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "avatar.png",
		Data:     testPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	if repo.avatars[avatar.ID].ProcessingStatus != domain.ProcessingStatusPending {
		t.Fatalf("processing status = %q", repo.avatars[avatar.ID].ProcessingStatus)
	}
}

func TestDeleteChecksOwnershipAndPublishesDeleteEvent(t *testing.T) {
	repo := newMemoryRepo()
	store := newMemoryStorage()
	pub := &memoryPublisher{}
	svc := NewAvatarService(repo, store, pub, 10<<20)
	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "owner",
		FileName: "avatar.png",
		Data:     testPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	repo.avatars[avatar.ID].ThumbnailS3Keys = map[string]string{"100x100": "thumbs/one.jpg"}

	err = svc.Delete(context.Background(), avatar.ID, "other")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}

	if err := svc.Delete(context.Background(), avatar.ID, "owner"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if repo.avatars[avatar.ID].DeletedAt == nil {
		t.Fatal("avatar was not soft deleted")
	}
	if len(pub.deletes) != 1 {
		t.Fatalf("delete events = %+v", pub.deletes)
	}
	if len(pub.deletes[0].S3Keys) != 2 {
		t.Fatalf("delete event keys = %+v", pub.deletes[0].S3Keys)
	}
}

func TestDeleteStillSoftDeletesWhenPublishFails(t *testing.T) {
	repo := newMemoryRepo()
	pub := &memoryPublisher{err: errors.New("rabbit down")}
	svc := NewAvatarService(repo, newMemoryStorage(), pub, 10<<20)
	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "owner",
		FileName: "avatar.png",
		Data:     testPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if err := svc.Delete(context.Background(), avatar.ID, "owner"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if repo.avatars[avatar.ID].DeletedAt == nil {
		t.Fatal("avatar was not soft deleted")
	}
}

func TestGetImageUsesThumbnailWhenAvailable(t *testing.T) {
	repo := newMemoryRepo()
	store := newMemoryStorage()
	svc := NewAvatarService(repo, store, &memoryPublisher{}, 10<<20)
	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "avatar.png",
		Data:     testPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	repo.avatars[avatar.ID].ThumbnailS3Keys = map[string]string{"100x100": "thumbs/one.jpg"}
	store.objects["thumbs/one.jpg"] = storage.Object{Key: "thumbs/one.jpg", Data: []byte("thumb"), ContentType: "image/jpeg"}

	obj, err := svc.GetImage(context.Background(), avatar.ID, "100x100", "")
	if err != nil {
		t.Fatalf("GetImage() error = %v", err)
	}
	if string(obj.Data) != "thumb" || obj.ContentType != "image/jpeg" {
		t.Fatalf("unexpected image object: %+v", obj)
	}
}

func TestGetImageReturnsNotReadyForMissingThumbnail(t *testing.T) {
	svc := NewAvatarService(newMemoryRepo(), newMemoryStorage(), &memoryPublisher{}, 10<<20)
	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "avatar.png",
		Data:     testPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	_, err = svc.GetImage(context.Background(), avatar.ID, "100x100", "")
	if !errors.Is(err, domain.ErrNotReady) {
		t.Fatalf("expected ErrNotReady, got %v", err)
	}
}

func TestGetImageValidatesAvatarIDAndConvertsFormat(t *testing.T) {
	repo := newMemoryRepo()
	store := newMemoryStorage()
	svc := NewAvatarService(repo, store, &memoryPublisher{}, 10<<20)
	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "avatar.png",
		Data:     testPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	_, err = svc.GetImage(context.Background(), "not-a-uuid", "original", "")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid avatar id, got %v", err)
	}

	obj, err := svc.GetImage(context.Background(), avatar.ID, "original", "jpeg")
	if err != nil {
		t.Fatalf("GetImage(format=jpeg) error = %v", err)
	}
	if obj.ContentType != imageutil.MimeJPEG {
		t.Fatalf("content type = %q", obj.ContentType)
	}
}

func TestReadMethodsAndHelpers(t *testing.T) {
	repo := newMemoryRepo()
	store := newMemoryStorage()
	pub := &memoryPublisher{}
	svc := NewAvatarService(repo, store, pub, 10<<20)
	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "avatar",
		Data:     testPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	obj, err := svc.GetLatestUserImage(context.Background(), "user-123", "original", "")
	if err != nil {
		t.Fatalf("GetLatestUserImage() error = %v", err)
	}
	if len(obj.Data) == 0 {
		t.Fatal("expected latest user image data")
	}

	meta, err := svc.GetMetadata(context.Background(), avatar.ID)
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if meta.ID != avatar.ID {
		t.Fatalf("metadata = %+v", meta)
	}

	list, err := svc.ListUserAvatars(context.Background(), "user-123")
	if err != nil {
		t.Fatalf("ListUserAvatars() error = %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}

	if got := svc.URLForAvatar(avatar.ID); got != "/api/v1/avatars/"+avatar.ID {
		t.Fatalf("URLForAvatar() = %q", got)
	}
	if got := svc.URLForS3Key("key"); got != "/objects/key" {
		t.Fatalf("URLForS3Key() = %q", got)
	}
	if !IsInvalidInput(domain.ErrInvalidInput) {
		t.Fatal("IsInvalidInput() should match domain.ErrInvalidInput")
	}
}

func TestDeleteLatestByUserAndHealth(t *testing.T) {
	repo := newMemoryRepo()
	store := newMemoryStorage()
	pub := &memoryPublisher{}
	svc := NewAvatarService(repo, store, pub, 10<<20)
	if _, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "avatar.png",
		Data:     testPNG(t, 8, 8),
	}); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	if err := svc.DeleteLatestByUser(context.Background(), "user-123", "other"); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected forbidden, got %v", err)
	}
	if err := svc.DeleteLatestByUser(context.Background(), "user-123", "user-123"); err != nil {
		t.Fatalf("DeleteLatestByUser() error = %v", err)
	}
	if len(pub.deletes) != 1 {
		t.Fatalf("delete events = %+v", pub.deletes)
	}

	statuses := svc.Health(context.Background())
	if statuses["database"] != "ok" || statuses["s3"] != "ok" || statuses["rabbitmq"] != "ok" {
		t.Fatalf("health = %+v", statuses)
	}
}

func TestGetImageRejectsUnsupportedSize(t *testing.T) {
	repo := newMemoryRepo()
	store := newMemoryStorage()
	svc := NewAvatarService(repo, store, &memoryPublisher{}, 10<<20)
	avatar, err := svc.Upload(context.Background(), UploadInput{
		UserID:   "user-123",
		FileName: "avatar.png",
		Data:     testPNG(t, 8, 8),
	})
	if err != nil {
		t.Fatalf("Upload() error = %v", err)
	}
	_, err = svc.GetImage(context.Background(), avatar.ID, "42x42", "")
	if !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected invalid size, got %v", err)
	}
}

type memoryRepo struct {
	avatars map[string]*domain.Avatar
}

func newMemoryRepo() *memoryRepo {
	return &memoryRepo{avatars: map[string]*domain.Avatar{}}
}

func (r *memoryRepo) Create(_ context.Context, a *domain.Avatar) error {
	cp := *a
	r.avatars[a.ID] = &cp
	return nil
}

func (r *memoryRepo) GetByID(_ context.Context, id string) (*domain.Avatar, error) {
	a, ok := r.avatars[id]
	if !ok || a.DeletedAt != nil {
		return nil, domain.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (r *memoryRepo) GetLatestByUserID(_ context.Context, userID string) (*domain.Avatar, error) {
	for _, a := range r.avatars {
		if a.UserID == userID && a.DeletedAt == nil {
			cp := *a
			return &cp, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *memoryRepo) ListByUserID(_ context.Context, userID string) ([]domain.Avatar, error) {
	var result []domain.Avatar
	for _, a := range r.avatars {
		if a.UserID == userID && a.DeletedAt == nil {
			result = append(result, *a)
		}
	}
	return result, nil
}

func (r *memoryRepo) UpdateUploadStatus(_ context.Context, id string, status domain.UploadStatus) error {
	a, ok := r.avatars[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.UploadStatus = status
	return nil
}

func (r *memoryRepo) UpdateProcessingStatus(_ context.Context, id string, status domain.ProcessingStatus) error {
	a, ok := r.avatars[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.ProcessingStatus = status
	return nil
}

func (r *memoryRepo) UpdateThumbnailsAndStatus(_ context.Context, id string, thumbnails map[string]string, status domain.ProcessingStatus) error {
	a, ok := r.avatars[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.ThumbnailS3Keys = thumbnails
	a.ProcessingStatus = status
	return nil
}

func (r *memoryRepo) SoftDelete(_ context.Context, id string) error {
	a, ok := r.avatars[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := a.CreatedAt
	a.DeletedAt = &now
	return nil
}

func (r *memoryRepo) Ping(context.Context) error {
	return nil
}

type memoryStorage struct {
	objects map[string]storage.Object
}

func newMemoryStorage() *memoryStorage {
	return &memoryStorage{objects: map[string]storage.Object{}}
}

func (s *memoryStorage) EnsureBucket(context.Context) error {
	return nil
}

func (s *memoryStorage) Upload(_ context.Context, key string, data []byte, contentType string) error {
	s.objects[key] = storage.Object{Key: key, Data: append([]byte(nil), data...), ContentType: contentType}
	return nil
}

func (s *memoryStorage) Download(_ context.Context, key string) (storage.Object, error) {
	obj, ok := s.objects[key]
	if !ok {
		return storage.Object{}, domain.ErrNotFound
	}
	return obj, nil
}

func (s *memoryStorage) Delete(_ context.Context, key string) error {
	delete(s.objects, key)
	return nil
}

func (s *memoryStorage) DeleteMany(ctx context.Context, keys []string) error {
	for _, key := range keys {
		_ = s.Delete(ctx, key)
	}
	return nil
}

func (s *memoryStorage) Ping(context.Context) error {
	return nil
}

func (s *memoryStorage) PublicURL(key string) string {
	return "/objects/" + key
}

type memoryPublisher struct {
	uploads []domain.AvatarUploadEvent
	deletes []domain.AvatarDeleteEvent
	err     error
}

func (p *memoryPublisher) PublishUpload(_ context.Context, event domain.AvatarUploadEvent) error {
	if p.err != nil {
		return p.err
	}
	p.uploads = append(p.uploads, event)
	return nil
}

func (p *memoryPublisher) PublishDelete(_ context.Context, event domain.AvatarDeleteEvent) error {
	if p.err != nil {
		return p.err
	}
	p.deletes = append(p.deletes, event)
	return nil
}

func (p *memoryPublisher) Ping() error {
	return p.err
}

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
