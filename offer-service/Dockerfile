# Build context is the REPOSITORY ROOT, matching every other service so one compose
# file builds them all the same way:
#   docker build -f offer-service/Dockerfile .
FROM python:3.13-slim-bookworm AS builder
COPY --from=ghcr.io/astral-sh/uv:0.12.5 /uv /usr/local/bin/uv

# The builder works in /app, the same path the runtime image uses. A virtualenv bakes
# absolute paths into its script shebangs, so building at /build and copying to /app
# would leave every entry point pointing at a directory that no longer exists.
WORKDIR /app
# UV_PYTHON_DOWNLOADS=never pins the venv to the base image's interpreter rather than a
# standalone build uv would otherwise fetch, so the venv still works in the runtime
# stage. Bytecode is compiled ahead of time to keep first-request latency down.
ENV UV_COMPILE_BYTECODE=1 \
    UV_LINK_MODE=copy \
    UV_PYTHON_DOWNLOADS=never

# Manifests first: dependency resolution caches independently of source changes.
COPY offer-service/pyproject.toml offer-service/uv.lock ./
RUN uv sync --frozen --no-dev

COPY offer-service/app ./app
COPY offer-service/migrations ./migrations
COPY offer-service/alembic.ini ./

FROM python:3.13-slim-bookworm
WORKDIR /app

RUN groupadd --system marketplace \
    && useradd --system --gid marketplace --uid 1001 --create-home offer

COPY --from=builder --chown=offer:marketplace /app /app
ENV PATH="/app/.venv/bin:$PATH" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1

USER offer
EXPOSE 8085

# The image carries no wget or curl; the venv's own interpreter backs the healthcheck.
HEALTHCHECK --interval=10s --timeout=5s --retries=12 --start-period=15s \
    CMD python -c "import urllib.request,sys; sys.exit(0 if urllib.request.urlopen('http://localhost:8085/health', timeout=4).status == 200 else 1)"

CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8085"]
