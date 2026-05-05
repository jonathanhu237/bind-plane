.PHONY: api worker frontend-dev test lint format openspec-validate

api:
	uv run fastapi dev

worker:
	uv run python -m bind_plane.worker.main

frontend-dev:
	cd frontend && npm run dev

test:
	uv run pytest
	cd frontend && npm run test

lint:
	uv run ruff check .
	cd frontend && npm run lint

format:
	uv run ruff format .
	cd frontend && npm run format

openspec-validate:
	openspec validate add-ipv4-release-workflow
