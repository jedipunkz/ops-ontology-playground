.PHONY: up down seed agent test

up:
	docker compose up --build -d

down:
	docker compose down

seed:
	go run ./cmd/seed

agent:
	go run ./cmd/agent console

test:
	go test ./...
