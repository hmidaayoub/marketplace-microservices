"""Test harness.

Runs against a real PostgreSQL with the real Alembic migrations applied, for the same
reason the other modules do: SQLite would not execute gen_random_uuid(), the CHECK
constraints or the regex constraint the code relies on.

DATABASE_URL short-circuits the container so the suite can also run against a Postgres
that is already up.
"""

import os
import uuid
from collections.abc import AsyncIterator, Iterator
from datetime import UTC, datetime, timedelta

import jwt
import pytest
from httpx import ASGITransport, AsyncClient

TEST_SECRET = "dev-secret-key-change-in-production-min-256-bits"
TEST_INTERNAL_KEY = "test-internal-api-key"


@pytest.fixture(scope="session")
def database_url() -> Iterator[str]:
    existing = os.getenv("DATABASE_URL")
    if existing:
        yield existing
        return

    try:  # module moved in testcontainers 4.x; support both
        from testcontainers.community.postgres import PostgresContainer
    except ImportError:  # pragma: no cover
        from testcontainers.postgres import PostgresContainer

    with PostgresContainer("postgres:15-alpine", dbname="notification_db") as postgres:
        yield postgres.get_connection_url()


@pytest.fixture(scope="session", autouse=True)
def configure_environment(database_url: str) -> Iterator[None]:
    os.environ["DATABASE_URL"] = database_url
    os.environ["JWT_SECRET"] = TEST_SECRET
    os.environ["INTERNAL_API_KEY"] = TEST_INTERNAL_KEY

    from app.config import get_settings

    get_settings.cache_clear()
    yield
    get_settings.cache_clear()


@pytest.fixture(scope="session", autouse=True)
def migrated(configure_environment: None) -> None:
    from app.migrate import run_migrations

    run_migrations()


@pytest.fixture
async def client(migrated: None) -> AsyncIterator[AsyncClient]:
    from sqlalchemy import text

    from app.db import get_engine
    from app.main import create_app

    app = create_app()

    async with (
        AsyncClient(
            transport=ASGITransport(app=app), base_url="http://notification.test"
        ) as http_client,
        app.router.lifespan_context(app),
    ):
        async with get_engine().begin() as connection:
            await connection.execute(text("TRUNCATE notification"))
        yield http_client


def make_token(
    user_id: uuid.UUID,
    role: str,
    *,
    secret: str = TEST_SECRET,
    algorithm: str = "HS384",
    expires_in: timedelta = timedelta(hours=1),
    extra_claims: dict | None = None,
) -> str:
    claims = {
        "sub": str(user_id),
        "role": role,
        "email": "user@test.com",
        "iat": datetime.now(UTC),
        "exp": datetime.now(UTC) + expires_in,
    }
    claims.update(extra_claims or {})
    return jwt.encode(claims, secret, algorithm=algorithm)


def auth(token: str) -> dict[str, str]:
    return {"Authorization": f"Bearer {token}"}


def internal_headers(key: str = TEST_INTERNAL_KEY) -> dict[str, str]:
    return {"X-Internal-Api-Key": key}


def notification_body(user_id: uuid.UUID, **overrides) -> dict:
    """A valid REQUEST_JOINED payload, the shape request-service sends."""
    body = {
        "userId": str(user_id),
        "type": "REQUEST_JOINED",
        "title": "Someone joined your request",
        "message": "A customer joined the request for an Espresso Machine.",
    }
    body.update(overrides)
    return body
