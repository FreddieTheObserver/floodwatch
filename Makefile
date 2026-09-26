# Tools run as pinned one-offs rather than entering go.mod, so their
# dependencies never reach the service build.
SQLC        := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@v0.8.1

export FLOODWATCH_DSN ?= postgres://floodwatch:floodwatch@localhost:5433/floodwatch?sslmode=disable

.PHONY: all build test vet lint sqlc sqlc-diff check db-up db-down psql run clean

all: check build

build:
	go build -o bin/ ./cmd/...

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	$(STATICCHECK) ./...

sqlc:
	$(SQLC) generate

sqlc-diff:
	$(SQLC) diff

check: vet lint sqlc-diff test

db-up:
	docker compose up -d --wait

db-down:
	docker compose down

psql:
	docker compose exec postgres psql -U floodwatch -d floodwatch

run:
	go run ./cmd/floodwatch

clean:
	rm -rf bin
