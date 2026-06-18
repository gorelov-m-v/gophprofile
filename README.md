# GophProfile

GophProfile is a Go microservice for uploading, storing, processing and serving user avatars.

## Stack

- Go 1.25+
- Chi HTTP router
- PostgreSQL for avatar metadata
- MinIO/S3 for image files
- RabbitMQ for asynchronous thumbnail and delete jobs
- OpenTelemetry, Prometheus, Jaeger, Grafana and Loki for observability
- Docker Compose for local development
- Kubernetes and Helm for deployment

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
GET    /metrics
```

Uploads require `X-User-ID` and a multipart field named `file` or `image`. JPEG, PNG and WebP are accepted up to 10 MB, with an additional guard against oversized image dimensions.

Image reads support `size=original|100x100|300x300`. Thumbnail requests return `404` until the worker has produced the requested size instead of silently returning the original image. `format=jpeg|png` converts the response; `format=webp` is returned only when the stored object is already WebP.

## Run Locally

```bash
docker compose up --build
```

Open:

- Web UI: http://localhost:8080/ or http://localhost:8080/web/upload
- Prometheus metrics: http://localhost:8080/metrics
- MinIO console: http://localhost:9001
- RabbitMQ console: http://localhost:15672
- Jaeger traces: http://localhost:16686
- Prometheus: http://localhost:9090
- Grafana: http://localhost:3000
- Alertmanager: http://localhost:9093

## Kubernetes

Build and push the image before deploying to a cluster:

```bash
docker build -f docker/Dockerfile -t gorelov-m-v/gophprofile:latest .
docker push gorelov-m-v/gophprofile:latest
```

Plain manifests:

```bash
kubectl apply -k k8s/base
kubectl -n gophprofile port-forward svc/gophprofile-server 8080:80
```

Helm:

```bash
helm upgrade --install gophprofile ./helm/gophprofile \
  --namespace gophprofile \
  --create-namespace \
  --values helm/gophprofile/values-dev.yaml
```

For real environments, override `secret.*`, `config.s3PublicURL`, `config.otelEndpoint` and `ingress.hosts` in a private values file. The chart creates server and worker deployments, services, ingress, HPA, ServiceMonitor, NetworkPolicy, PodDisruptionBudget, RBAC, service account and a migration hook job.

In Kubernetes, schema migrations are executed by the `/app/migrate` job. Server and worker pods receive `RUN_MIGRATIONS=false`, so rollout does not start competing migration attempts from application replicas.

Default credentials:

- PostgreSQL: `gophprofile / gophprofile`
- MinIO: `minioadmin / minioadmin`
- RabbitMQ: `guest / guest`
- Grafana: `admin / admin`

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
go build ./cmd/migrate
helm lint ./helm/gophprofile
helm template gophprofile ./helm/gophprofile --values helm/gophprofile/values-dev.yaml
```

For local Docker Compose development, startup migrations remain enabled by default and use a PostgreSQL advisory lock. In Kubernetes, migrations are handled by a dedicated job and disabled in server and worker pods.

The worker is idempotent: completed avatar jobs are skipped, and transient processing errors are retried with exponential backoff before the message is rejected. RabbitMQ is still the primary async path, while the worker also periodically scans PostgreSQL for uploaded avatars or deleted avatars whose broker event was missed and recovers those jobs.

## Observability

The server and worker export traces through OTLP to the OpenTelemetry Collector, which forwards them to Jaeger. HTTP requests, service methods, PostgreSQL operations, S3 operations and RabbitMQ publish/consume flows are traced with context propagation through AMQP headers.

Prometheus scrapes the server and worker metrics endpoints. The Grafana dashboard `GophProfile Overview` is provisioned automatically and shows RED metrics, upload KPIs, worker jobs, storage usage, database pool usage, queue depth and application logs.

Application logs are JSON slog records written to stdout and to `/app/logs/*.log`. Promtail ships those files to Loki, including `trace_id` and `span_id` labels when a log is emitted inside a trace.

## Documentation

- OpenAPI: `docs/openapi.yaml`
- Architecture: `docs/architecture.md`
