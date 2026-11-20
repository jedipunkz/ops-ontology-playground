.PHONY: up down seed demo test

up:
	docker compose up --build -d

down:
	docker compose down

# Recreates the ops database. Deletes every recorded hypothesis.
seed:
	go run ./cmd/seed

# Runs Claude Code headless three times. Uses your Claude Code quota.
demo:
	./scripts/demo.sh

test:
	go test ./...
