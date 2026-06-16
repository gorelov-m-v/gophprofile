package config

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	ServiceName      string
	ServerAddr       string
	MetricsAddr      string
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	ShutdownTimeout  time.Duration
	DatabaseURL      string
	MigrationsPath   string
	S3Endpoint       string
	S3AccessKey      string
	S3SecretKey      string
	S3Bucket         string
	S3UseSSL         bool
	S3PublicURL      string
	RabbitMQURL      string
	RabbitMQExchange string
	WebDir           string
	MaxUploadSize    int64
	OTLPEndpoint     string
	LogLevel         string
	LogFile          string
}

func Default() Config {
	return Config{
		ServiceName:      "gophprofile",
		ServerAddr:       ":8080",
		MetricsAddr:      ":9091",
		ReadTimeout:      10 * time.Second,
		WriteTimeout:     30 * time.Second,
		ShutdownTimeout:  10 * time.Second,
		DatabaseURL:      "postgres://gophprofile:gophprofile@localhost:5432/gophprofile?sslmode=disable",
		MigrationsPath:   "migrations",
		S3Endpoint:       "localhost:9000",
		S3AccessKey:      "minioadmin",
		S3SecretKey:      "minioadmin",
		S3Bucket:         "avatars",
		S3UseSSL:         false,
		S3PublicURL:      "http://localhost:9000/avatars",
		RabbitMQURL:      "amqp://guest:guest@localhost:5672/",
		RabbitMQExchange: "avatars.exchange",
		WebDir:           "web/static",
		MaxUploadSize:    10 << 20,
		OTLPEndpoint:     "",
		LogLevel:         "info",
		LogFile:          "",
	}
}

func Load(args []string) (Config, error) {
	cfg := Default()

	cfg.ServiceName = envString("SERVICE_NAME", cfg.ServiceName)
	cfg.ServerAddr = envString("SERVER_ADDR", cfg.ServerAddr)
	cfg.MetricsAddr = envString("METRICS_ADDR", cfg.MetricsAddr)
	cfg.ReadTimeout = envDuration("HTTP_READ_TIMEOUT", cfg.ReadTimeout)
	cfg.WriteTimeout = envDuration("HTTP_WRITE_TIMEOUT", cfg.WriteTimeout)
	cfg.ShutdownTimeout = envDuration("HTTP_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout)
	cfg.DatabaseURL = firstNonEmpty(os.Getenv("DATABASE_URL"), os.Getenv("DB_URL"), cfg.DatabaseURL)
	cfg.MigrationsPath = envString("MIGRATIONS_PATH", cfg.MigrationsPath)
	cfg.S3Endpoint = envString("S3_ENDPOINT", cfg.S3Endpoint)
	cfg.S3AccessKey = firstNonEmpty(os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_KEY"), cfg.S3AccessKey)
	cfg.S3SecretKey = firstNonEmpty(os.Getenv("S3_SECRET_KEY"), os.Getenv("S3_SECRET"), cfg.S3SecretKey)
	cfg.S3Bucket = envString("S3_BUCKET", cfg.S3Bucket)
	cfg.S3UseSSL = envBool("S3_USE_SSL", cfg.S3UseSSL)
	cfg.S3PublicURL = envString("S3_PUBLIC_URL", cfg.S3PublicURL)
	cfg.RabbitMQURL = firstNonEmpty(os.Getenv("RABBITMQ_URL"), os.Getenv("RABBIT_URL"), cfg.RabbitMQURL)
	cfg.RabbitMQExchange = envString("RABBITMQ_EXCHANGE", cfg.RabbitMQExchange)
	cfg.WebDir = envString("WEB_DIR", cfg.WebDir)
	cfg.MaxUploadSize = envInt64("MAX_UPLOAD_SIZE", cfg.MaxUploadSize)
	cfg.OTLPEndpoint = envString("OTEL_EXPORTER_OTLP_ENDPOINT", cfg.OTLPEndpoint)
	cfg.LogLevel = envString("LOG_LEVEL", cfg.LogLevel)
	cfg.LogFile = envString("LOG_FILE", cfg.LogFile)

	fs := flag.NewFlagSet("gophprofile", flag.ContinueOnError)
	fs.StringVar(&cfg.ServiceName, "service-name", cfg.ServiceName, "service name for observability")
	fs.StringVar(&cfg.ServerAddr, "addr", cfg.ServerAddr, "HTTP listen address")
	fs.StringVar(&cfg.MetricsAddr, "metrics-addr", cfg.MetricsAddr, "metrics HTTP listen address")
	fs.StringVar(&cfg.DatabaseURL, "database-url", cfg.DatabaseURL, "PostgreSQL DSN")
	fs.StringVar(&cfg.MigrationsPath, "migrations-path", cfg.MigrationsPath, "directory with SQL migrations")
	fs.StringVar(&cfg.S3Endpoint, "s3-endpoint", cfg.S3Endpoint, "S3-compatible endpoint")
	fs.StringVar(&cfg.S3AccessKey, "s3-access-key", cfg.S3AccessKey, "S3 access key")
	fs.StringVar(&cfg.S3SecretKey, "s3-secret-key", cfg.S3SecretKey, "S3 secret key")
	fs.StringVar(&cfg.S3Bucket, "s3-bucket", cfg.S3Bucket, "S3 bucket")
	fs.BoolVar(&cfg.S3UseSSL, "s3-use-ssl", cfg.S3UseSSL, "use TLS for S3 endpoint")
	fs.StringVar(&cfg.S3PublicURL, "s3-public-url", cfg.S3PublicURL, "public S3 bucket URL")
	fs.StringVar(&cfg.RabbitMQURL, "rabbitmq-url", cfg.RabbitMQURL, "RabbitMQ URL")
	fs.StringVar(&cfg.RabbitMQExchange, "rabbitmq-exchange", cfg.RabbitMQExchange, "RabbitMQ exchange")
	fs.StringVar(&cfg.WebDir, "web-dir", cfg.WebDir, "static web directory")
	fs.Int64Var(&cfg.MaxUploadSize, "max-upload-size", cfg.MaxUploadSize, "maximum upload size in bytes")
	fs.StringVar(&cfg.OTLPEndpoint, "otel-endpoint", cfg.OTLPEndpoint, "OTLP gRPC endpoint")
	fs.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level")
	fs.StringVar(&cfg.LogFile, "log-file", cfg.LogFile, "optional JSON log file")
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("database url is required")
	}
	if cfg.S3Endpoint == "" || cfg.S3AccessKey == "" || cfg.S3SecretKey == "" || cfg.S3Bucket == "" {
		return Config{}, fmt.Errorf("s3 endpoint, credentials and bucket are required")
	}
	if cfg.RabbitMQURL == "" {
		return Config{}, fmt.Errorf("rabbitmq url is required")
	}
	if cfg.MaxUploadSize <= 0 {
		return Config{}, fmt.Errorf("max upload size must be positive")
	}

	return cfg, nil
}

func envString(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if parsed, err := time.ParseDuration(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseBool(value); err == nil {
			return parsed
		}
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if value := os.Getenv(key); value != "" {
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			return parsed
		}
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
