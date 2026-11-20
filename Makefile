.PHONY: up down seed agent agent-gemini test

up:
	docker compose up --build -d

down:
	docker compose down

seed:
	go run ./cmd/seed

agent:
	go run ./cmd/agent console

agent-gemini:
	MODEL_PROVIDER=gemini go run ./cmd/agent console

test:
	go test ./...
