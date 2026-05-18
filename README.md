# GophProfile

GophProfile is a Go microservice for uploading, storing, processing and serving user avatars.

## Stack

- Go 1.25+
- Chi HTTP router
- PostgreSQL for avatar metadata
- MinIO/S3 for image files
- RabbitMQ for asynchronous thumbnail and delete jobs
- Docker Compose for local development

## API

```http
POST   /api/v1/avatars
GET    /api/v1/avatars/{avatar_id}
GET    /api/v1/avatars/{avatar_id}/metadata
DELETE /api/v1/avatars/{avatar_id}
GET    /api/v1/users/{user_id}/avatar
GET    /api/v1/users/{user_id}/avatars
DELETE /api/v1/users/{user_id}/avatar
GET    /health
```

Uploads require `X-User-ID` and a multipart field named `file` or `image`. JPEG, PNG and WebP are accepted up to 10 MB, with an additional guard against oversized image dimensions.

Image reads support `size=original|100x100|300x300`. Thumbnail requests return `404` until the worker has produced the requested size instead of silently returning the original image. `format=jpeg|png` converts the response; `format=webp` is returned only when the stored object is already WebP.

## Run Locally

```bash
docker compose up --build
```

Open:

- Web UI: http://localhost:8080/ or http://localhost:8080/web/upload
- MinIO console: http://localhost:9001
- RabbitMQ console: http://localhost:15672

Default credentials:

- PostgreSQL: `gophprofile / gophprofile`
- MinIO: `minioadmin / minioadmin`
- RabbitMQ: `guest / guest`

## Curl Examples

```bash
curl -i -X POST http://localhost:8080/api/v1/avatars \
  -H "X-User-ID: user-123" \
  -F "file=@avatar.jpg"
```

```bash
curl -i http://localhost:8080/api/v1/users/user-123/avatars
```

```bash
curl -i http://localhost:8080/api/v1/users/user-123/avatar --output avatar.jpg
```

## Development

```bash
go test ./...
go test -cover ./...
go build ./cmd/server
go build ./cmd/worker
```

The server and worker both run SQL migrations on startup. The worker is idempotent: completed avatar jobs are skipped, and transient processing errors are retried with exponential backoff before the message is rejected.

Migrations use a PostgreSQL advisory lock so concurrent server/worker startup does not race. RabbitMQ is still the primary async path, while the worker also periodically scans PostgreSQL for uploaded avatars or deleted avatars whose broker event was missed and recovers those jobs.
