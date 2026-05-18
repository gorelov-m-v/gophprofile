.PHONY: test coverage build-server build-worker docker-up docker-down tidy

tidy:
	go mod tidy

test:
	go test -count=1 ./...

coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

build-server:
	go build -o bin/server ./cmd/server

build-worker:
	go build -o bin/worker ./cmd/worker

docker-up:
	docker compose up --build

docker-down:
	docker compose down -v
