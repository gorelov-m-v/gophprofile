package domain

import "testing"

func TestAvatarS3KeysReturnsOriginalAndThumbnails(t *testing.T) {
	avatar := Avatar{
		S3Key: "original",
		ThumbnailS3Keys: map[string]string{
			"100x100": "small",
			"empty":   "",
			"300x300": "medium",
		},
	}
	keys := avatar.S3Keys()
	if len(keys) != 3 {
		t.Fatalf("keys = %+v", keys)
	}
	if keys[0] != "original" {
		t.Fatalf("first key = %q", keys[0])
	}
}
