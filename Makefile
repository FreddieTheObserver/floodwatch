# Tools run as pinned one-offs rather than entering go.mod, so their
# dependencies never reach the service build.
SQLC        := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
STATICCHECK := go run honnef.co/go/tools/cmd/staticcheck@v0.8.1

export FLOODWATCH_DSN ?= postgres://floodwatch:floodwatch@localhost:5433/floodwatch?sslmode=disable

# Secrets such as the bot token live in an untracked .env, loaded only by the
# targets that start the service.
LOAD_ENV := if [ -f .env ]; then set -a; . ./.env; set +a; fi;

.PHONY: all build test fmt-check vet lint sqlc sqlc-diff check db-up db-down psql run serve up down clean

all: check build

build:
	go build -o bin/ ./cmd/...

test:
	go test -race ./...

# Fails listing the files gofmt would change, since gofmt itself exits 0.
fmt-check:
	@files=$$(gofmt -l .); if [ -n "$$files" ]; then echo "$$files"; exit 1; fi

vet:
	go vet ./...

lint:
	$(STATICCHECK) ./...

sqlc:
	$(SQLC) generate

sqlc-diff:
	$(SQLC) diff

check: fmt-check vet lint sqlc-diff test

db-up:
	docker compose up -d --wait

db-down:
	docker compose down

psql:
	docker compose exec postgres psql -U floodwatch -d floodwatch

run:
	@$(LOAD_ENV) go run ./cmd/floodwatch

# The built binary, as the long-running service uses it.
serve: build
	@$(LOAD_ENV) exec ./bin/floodwatch

# Runs the service detached in a tmux session, restarting it ten seconds after
# it exits for any reason, with its output appended to floodwatch.log. Pressing
# Ctrl-C inside the session therefore restarts it; make down stops it.
up: build
	@if tmux has-session -t floodwatch 2>/dev/null; then \
		echo "already running; watch it with: tmux attach -t floodwatch"; \
	else \
		tmux new-session -d -s floodwatch 'while true; do make serve 2>&1 | tee -a floodwatch.log; echo "floodwatch exited; restarting in 10 s" | tee -a floodwatch.log; sleep 10; done' && \
		echo "started; watch it with: tmux attach -t floodwatch"; \
	fi

# Stops the service, then pauses the watchdog once it has exited, so that a
# report still in flight cannot rearm it. The first report after make up does.
down: build
	@if tmux kill-session -t floodwatch 2>/dev/null; then \
		for _ in $$(seq 30); do pgrep -x floodwatch >/dev/null || break; sleep 0.5; done; \
		echo stopped; \
	else \
		echo "not running"; \
	fi
	@$(LOAD_ENV) ./bin/floodwatch pause-watchdog || echo "the watchdog will email you that floodwatch is down"

clean:
	rm -rf bin
