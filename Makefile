.PHONY: dev down logs test frontend-test frontend-e2e fmt tidy migrate-status

dev:
	docker compose up --build

down:
	docker compose down

logs:
	docker compose logs -f api worker frontend

test:
	go test ./...

frontend-test:
	cd frontend && npm run build && npm run lint

frontend-e2e:
	cd frontend && npm run test:e2e

fmt:
	go fmt ./...

tidy:
	go mod tidy

migrate-status:
	docker compose run --rm migrate /app/bin/migrate status
