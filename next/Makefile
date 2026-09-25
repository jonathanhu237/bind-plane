.PHONY: up down logs logs-admin logs-mailpit migrate-up migrate-down-one build test admin-install admin-dev admin-test

up:
	docker compose --profile development up -d api admin mailpit

down:
	docker compose down

logs:
	docker compose logs -f api

logs-admin:
	docker compose logs -f admin

logs-mailpit:
	docker compose logs -f mailpit

migrate-up:
	docker compose up -d postgres
	docker compose --profile tools run --rm migrate

migrate-down-one:
	docker compose --profile tools run --rm migrate down 1

build:
	docker compose build api admin migrate

test:
	cd api && go test ./...
	cd admin && pnpm test

admin-install:
	cd admin && pnpm install --ignore-scripts

admin-dev:
	cd admin && pnpm dev

admin-test:
	cd admin && pnpm test
