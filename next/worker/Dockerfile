FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim
WORKDIR /app
COPY pyproject.toml uv.lock README.md ./
COPY backend ./backend
RUN uv sync --frozen --no-dev
COPY next/worker /app/worker
ENV PATH="/app/.venv/bin:$PATH"
CMD ["python", "-m", "worker.main"]
