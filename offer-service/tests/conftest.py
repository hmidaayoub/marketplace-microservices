"""Test harness.

Runs against a real PostgreSQL with the real Alembic migrations applied, for the same
reason the other modules do: SQLite would not execute gen_random_uuid(), the CHECK
constraints or the regex constraint the code relies on.

DATABASE_URL short-circuits the container so the suite can also run against a Postgres
that is already up.
"""

import json
import os
import re
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

    with PostgresContainer("postgres:15-alpine", dbname="offer_db") as postgres:
        yield postgres.get_connection_url()


@pytest.fixture(scope="session", autouse=True)
def configure_environment(database_url: str) -> Iterator[None]:
    os.environ["DATABASE_URL"] = database_url
    os.environ["JWT_SECRET"] = TEST_SECRET
    os.environ["INTERNAL_API_KEY"] = TEST_INTERNAL_KEY
    # Pointed at a stub in-process; no real seller-service or request-service is started.
    os.environ["SELLER_SERVICE_URL"] = "http://seller.test"
    os.environ["REQUEST_SERVICE_URL"] = "http://request.test"
    os.environ["AUTH_SERVICE_URL"] = "http://auth.test"

    from app.config import get_settings

    get_settings.cache_clear()
    yield
    get_settings.cache_clear()


@pytest.fixture(scope="session", autouse=True)
def migrated(configure_environment: None) -> None:
    from app.migrate import run_migrations

    run_migrations()


class FakeUpstream:
    """Stands in for seller-service and request-service.

    Registered as the httpx transport on the app's client, so the real client code -
    URL building, the internal API key header, status handling - is still exercised.
    """

    def __init__(self) -> None:
        self.sellers: dict[uuid.UUID, uuid.UUID] = {}
        self.requests: dict[uuid.UUID, str] = {}
        self.admins: list[uuid.UUID] = []
        self.seen_api_keys: list[str | None] = []

        # request_item_key, roughly: request-service normalizes an item name before
        # matching on it, and the find-or-create is only interesting here because it can
        # return a request that already exists.
        self.requests_by_item: dict[str, uuid.UUID] = {}
        self.opened_items: list[dict] = []

        # Flipped by the tests that assert what an unreachable request-service costs.
        # Deliberately separate from count_reports_fail: an outage that stops a request
        # being looked up stops the offer outright, while one that only loses the count
        # report leaves the offer standing and the count to be repaired later. Those are
        # different behaviours, so they need different switches to pin them down.
        self.request_service_down = False
        self.count_reports_fail = False

        # The live-offer counts this service has reported, newest last per request. The
        # status of a request depends on them, and request-service cannot count offers
        # it cannot see - so what gets pushed is behaviour worth pinning down.
        self.offer_counts: dict[uuid.UUID, int] = {}
        self.reported_counts: list[tuple[uuid.UUID, int]] = []

    def add_admin(self, user_id: uuid.UUID) -> None:
        self.admins.append(user_id)

    def add_seller(self, user_id: uuid.UUID, seller_id: uuid.UUID) -> None:
        self.sellers[user_id] = seller_id

    def add_request(
        self, request_id: uuid.UUID, status: str = "OPEN", item_name: str | None = None
    ) -> None:
        self.requests[request_id] = status
        if item_name is not None:
            self.requests_by_item[_item_key(item_name)] = request_id

    async def handler(self, request):  # httpx.Request -> httpx.Response
        import httpx

        self.seen_api_keys.append(request.headers.get("X-Internal-Api-Key"))
        path = request.url.path

        if path.startswith("/internal/sellers/by-user/"):
            raw = path.rsplit("/", 1)[-1]
            seller_id = self.sellers.get(uuid.UUID(raw))
            if seller_id is None:
                return httpx.Response(404, json={"message": "not found", "status": 404})
            return httpx.Response(200, json={"sellerId": str(seller_id), "userId": raw})

        if path == "/internal/users/by-role/ADMIN":
            return httpx.Response(
                200, json=[{"userId": str(a), "role": "ADMIN"} for a in self.admins]
            )

        if path.startswith("/internal/requests") and self.request_service_down:
            return httpx.Response(500, json={"message": "boom", "status": 500})

        if path == "/internal/requests" and request.method == "POST":
            body = json.loads(request.content)
            self.opened_items.append(body)
            key = _item_key(body["itemName"])

            # Find-or-create, as request-service implements it: an item that already has
            # a request hands that one back, and only a new item opens one.
            existing = self.requests_by_item.get(key)
            if existing is not None:
                return httpx.Response(200, json=self._request_body(existing))

            opened = uuid.uuid4()
            self.requests_by_item[key] = opened
            # INACTIVE with nobody on it, which is what a request with no participants is.
            self.requests[opened] = "INACTIVE"
            return httpx.Response(
                201, json=self._request_body(opened, total_customers=0, total_quantity=0)
            )

        if path.endswith("/offers/count") and request.method == "PUT":
            if self.count_reports_fail:
                return httpx.Response(500, json={"message": "boom", "status": 500})
            request_id = uuid.UUID(path.split("/")[3])
            count = json.loads(request.content)["totalOffers"]
            self.offer_counts[request_id] = count
            self.reported_counts.append((request_id, count))
            return httpx.Response(200, json=self._request_body(request_id))

        if path.startswith("/internal/requests/"):
            raw = path.rsplit("/", 1)[-1]
            status = self.requests.get(uuid.UUID(raw))
            if status is None:
                return httpx.Response(404, json={"message": "not found", "status": 404})
            return httpx.Response(200, json=self._request_body(uuid.UUID(raw), status=status))

        return httpx.Response(500, json={"message": "unexpected call", "status": 500})

    def _request_body(
        self,
        request_id: uuid.UUID,
        *,
        status: str | None = None,
        total_customers: int = 2,
        total_quantity: int = 8,
    ) -> dict:
        return {
            "requestId": str(request_id),
            "status": status or self.requests.get(request_id, "OPEN"),
            "totalCustomers": total_customers,
            "totalQuantity": total_quantity,
        }


def _item_key(item_name: str) -> str:
    """Stands in for request-service's request_item_key: case and punctuation fold away.

    Deliberately cruder than the real one, which also resolves abbreviations and drops
    filler. What matters to this suite is only that "Espresso Machine" and "espresso
    machine" are one item, so the stub does not have to be right about the rest - and
    the real normalization is pinned down where it lives, in request-service's own suite.
    """
    return re.sub(r"[^a-z0-9]+", " ", item_name.lower()).strip()


class RecordingPublisher:
    """Stands in for the broker: the relay hands it what it would have sent.
    The AMQP path itself is exercised end to end against a real RabbitMQ in the stack."""

    def __init__(self) -> None:
        self.published: list[tuple[str, list[dict]]] = []

    async def publish_event(self, event_id, routing_key: str, notifications: list[dict]) -> None:
        # Mirrors the real Publisher, which returns early rather than sending an event
        # with no recipients. A double that is more permissive than the thing it stands
        # in for hides exactly the behaviour the tests are meant to pin down.
        if not notifications:
            return
        self.published.append((routing_key, notifications))

    async def aclose(self) -> None:
        return None


class RecordingRelay:
    """Replaces the background relay so the suite never reaches for a broker.

    The real relay is stopped as soon as the app starts; its poll loop would otherwise
    spend the whole suite trying to connect. Tests that care about relaying call
    drain(), which runs the real Relay._drain against the recording publisher - so the
    outbox query, the marking and the ordering are all still the production code.
    """

    def __init__(self, publisher: RecordingPublisher) -> None:
        self.publisher = publisher
        self.wakes = 0

    def wake(self) -> None:
        self.wakes += 1

    async def drain(self) -> int:
        from app.db import get_sessionmaker
        from app.events import Relay

        return await Relay(get_sessionmaker(), self.publisher)._drain()

    async def aclose(self) -> None:
        return None


@pytest.fixture
async def publisher() -> RecordingPublisher:
    return RecordingPublisher()


@pytest.fixture
async def upstream() -> FakeUpstream:
    return FakeUpstream()


@pytest.fixture
async def client(
    migrated: None, upstream: FakeUpstream, publisher: RecordingPublisher
) -> AsyncIterator[AsyncClient]:
    import httpx
    from sqlalchemy import text

    from app.db import get_engine
    from app.main import create_app

    app = create_app()

    async with (
        AsyncClient(transport=ASGITransport(app=app), base_url="http://offer.test") as http_client,
        app.router.lifespan_context(app),
    ):
        # Swap the real outbound transport for the stub, keeping the client code.
        app.state.clients._client = httpx.AsyncClient(
            transport=httpx.MockTransport(upstream.handler),
            headers={"X-Internal-Api-Key": TEST_INTERNAL_KEY},
        )
        # Stop the real relay before its poll loop reaches for a broker, and swap both
        # it and the publisher for recorders.
        await app.state.relay.aclose()
        app.state.publisher = publisher
        app.state.relay = RecordingRelay(publisher)
        # Handed to tests that need to drive a drain or read the wake count.
        http_client.relay = app.state.relay
        async with get_engine().begin() as connection:
            await connection.execute(text("TRUNCATE offer, notification_outbox"))
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
