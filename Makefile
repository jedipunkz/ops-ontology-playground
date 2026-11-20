.PHONY: up down seed test

up:
	docker compose up --build -d

down:
	docker compose down

seed:
	go run ./cmd/seed

test:
	go test ./...
