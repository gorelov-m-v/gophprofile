package domain

import (
	"errors"
	"time"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrNotReady     = errors.New("not ready")
)

type UploadStatus string

const (
	UploadStatusUploading UploadStatus = "uploading"
	UploadStatusUploaded  UploadStatus = "uploaded"
	UploadStatusFailed    UploadStatus = "failed"
)

type ProcessingStatus string

const (
	ProcessingStatusPending    ProcessingStatus = "pending"
	ProcessingStatusProcessing ProcessingStatus = "processing"
	ProcessingStatusCompleted  ProcessingStatus = "completed"
	ProcessingStatusFailed     ProcessingStatus = "failed"
)

type CleanupStatus string

const (
	CleanupStatusPending   CleanupStatus = "pending"
	CleanupStatusCompleted CleanupStatus = "completed"
	CleanupStatusFailed    CleanupStatus = "failed"
)

type Dimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Thumbnail struct {
	Size string `json:"size"`
	URL  string `json:"url"`
}

type Avatar struct {
	ID               string
	UserID           string
	FileName         string
	MimeType         string
	SizeBytes        int64
	S3Key            string
	ThumbnailS3Keys  map[string]string
	Width            int
	Height           int
	UploadStatus     UploadStatus
	ProcessingStatus ProcessingStatus
	CleanupStatus    CleanupStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
	DeletedAt        *time.Time
}

func (a Avatar) S3Keys() []string {
	keys := make([]string, 0, 1+len(a.ThumbnailS3Keys))
	if a.S3Key != "" {
		keys = append(keys, a.S3Key)
	}
	for _, key := range a.ThumbnailS3Keys {
		if key != "" {
			keys = append(keys, key)
		}
	}
	return keys
}

type AvatarUploadEvent struct {
	MessageID string `json:"message_id"`
	AvatarID  string `json:"avatar_id"`
	UserID    string `json:"user_id"`
	S3Key     string `json:"s3_key"`
}

type ProcessingOp string

const (
	ProcessingOpThumbnail100 ProcessingOp = "thumbnail_100x100"
	ProcessingOpThumbnail300 ProcessingOp = "thumbnail_300x300"
)

type AvatarProcessEvent struct {
	MessageID  string         `json:"message_id"`
	AvatarID   string         `json:"avatar_id"`
	Operations []ProcessingOp `json:"operations"`
}

type AvatarDeleteEvent struct {
	MessageID string   `json:"message_id"`
	AvatarID  string   `json:"avatar_id"`
	S3Keys    []string `json:"s3_keys"`
}

type ThumbnailSpec struct {
	Name   string
	Width  int
	Height int
}

var DefaultThumbnails = []ThumbnailSpec{
	{Name: "100x100", Width: 100, Height: 100},
	{Name: "300x300", Width: 300, Height: 300},
}
