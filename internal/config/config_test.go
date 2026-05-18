package config

import (
	"testing"
	"time"
)

func TestLoadAppliesEnvAndFlags(t *testing.T) {
	t.Setenv("SERVER_ADDR", ":8081")
	t.Setenv("DATABASE_URL", "postgres://env")
	t.Setenv("S3_ENDPOINT", "minio:9000")
	t.Setenv("S3_ACCESS_KEY", "access")
	t.Setenv("S3_SECRET_KEY", "secret")
	t.Setenv("S3_BUCKET", "avatars")
	t.Setenv("S3_USE_SSL", "true")
	t.Setenv("RABBITMQ_URL", "amqp://env")
	t.Setenv("HTTP_READ_TIMEOUT", "3s")
	t.Setenv("MAX_UPLOAD_SIZE", "42")

	cfg, err := Load([]string{"-addr", ":9090", "-s3-use-ssl=false"})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ServerAddr != ":9090" {
		t.Fatalf("ServerAddr = %q", cfg.ServerAddr)
	}
	if cfg.DatabaseURL != "postgres://env" || cfg.RabbitMQURL != "amqp://env" {
		t.Fatalf("unexpected env config: %+v", cfg)
	}
	if cfg.S3UseSSL {
		t.Fatal("flag should override S3_USE_SSL")
	}
	if cfg.ReadTimeout != 3*time.Second {
		t.Fatalf("ReadTimeout = %v", cfg.ReadTimeout)
	}
	if cfg.MaxUploadSize != 42 {
		t.Fatalf("MaxUploadSize = %d", cfg.MaxUploadSize)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cfg := Default()
	if cfg.ServerAddr == "" || cfg.MaxUploadSize <= 0 {
		t.Fatalf("bad defaults: %+v", cfg)
	}

	_, err := Load([]string{"-max-upload-size", "0"})
	if err == nil {
		t.Fatal("expected max upload size validation error")
	}

	_, err = Load([]string{"-bad-flag"})
	if err == nil {
		t.Fatal("expected flag parsing error")
	}
}
