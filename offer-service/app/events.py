"""Notification events: the outbox writer and the relay that drains it.

Nothing publishes inline. An event is written to notification_outbox inside the
transaction that stores the offer, and the relay is the only thing that sends it. That
is what makes a broker outage cost latency rather than the notification itself: the row
is already durable, and the relay keeps retrying until it is sent.
"""

import asyncio
import contextlib
import json
import logging
import uuid
from collections.abc import Sequence
from datetime import UTC, datetime

import aio_pika
from sqlalchemy import select, update
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker

from app.models import NotificationOutbox

log = logging.getLogger(__name__)

EXCHANGE = "marketplace.events"
DEAD_LETTER_EXCHANGE = "marketplace.events.dlx"
QUEUE = "notification.events"
DEAD_LETTER_QUEUE = "notification.events.dlq"
BINDING_KEY = "#"

KEY_OFFER_CREATED = "offer.created"

# Has to be set explicitly: a stopped broker on a Docker network blackholes the
# connection rather than refusing it, so an unbounded dial waits for a broker nobody is
# waiting on.
_DIAL_TIMEOUT_SECONDS = 0.7

# connect_robust retries internally and forever, which is right for a long-lived
# consumer and wrong inside a publish: a broker that is unreachable, or reachable but
# refusing the credentials, would block the relay's drain indefinitely instead of
# failing so that the next tick can retry. The whole attempt is bounded instead.
_PUBLISH_TIMEOUT_SECONDS = 3.0


class Publisher:
    """Owns one lazily-established connection.

    Lazy rather than dialled at startup because the service must come up whether or not
    the broker is there, and reconnecting on demand covers a broker that restarts
    underneath a running service.
    """

    def __init__(self, url: str, source: str) -> None:
        self._url = url
        self._source = source
        self._connection: aio_pika.abc.AbstractRobustConnection | None = None
        self._exchange: aio_pika.abc.AbstractExchange | None = None

    async def aclose(self) -> None:
        if self._connection is not None:
            await self._connection.close()
        self._connection = None
        self._exchange = None

    async def _publish_once(self, message: aio_pika.Message, routing_key: str) -> None:
        exchange = await self._acquire()
        await exchange.publish(message, routing_key=routing_key)

    async def _acquire(self) -> aio_pika.abc.AbstractExchange:
        if self._exchange is not None and not self._connection.is_closed:
            return self._exchange

        self._connection = await aio_pika.connect_robust(self._url, timeout=_DIAL_TIMEOUT_SECONDS)
        channel = await self._connection.channel()

        # Declared here as well as in the consumer. Every declaration is idempotent and
        # identical, so a fresh broker needs no setup and it does not matter which side
        # connects first - and publishing to an exchange that does not exist yet would
        # otherwise be silently dropped.
        dead_letter_exchange = await channel.declare_exchange(
            DEAD_LETTER_EXCHANGE, aio_pika.ExchangeType.FANOUT, durable=True
        )
        dead_letter_queue = await channel.declare_queue(DEAD_LETTER_QUEUE, durable=True)
        await dead_letter_queue.bind(dead_letter_exchange)

        exchange = await channel.declare_exchange(
            EXCHANGE, aio_pika.ExchangeType.TOPIC, durable=True
        )
        queue = await channel.declare_queue(
            QUEUE, durable=True, arguments={"x-dead-letter-exchange": DEAD_LETTER_EXCHANGE}
        )
        await queue.bind(exchange, routing_key=BINDING_KEY)

        self._exchange = exchange
        return exchange

    async def publish_event(
        self, event_id: uuid.UUID, routing_key: str, notifications: list[dict]
    ) -> None:
        """Publishes one event under a caller-supplied id.

        The id comes from the outbox row rather than being minted here, so a relay that
        republishes a row after dying mid-commit sends the same id twice - which the
        consumer's processed_event table recognises as a redelivery instead of
        duplicating the notification.

        Raises on failure: the relay needs to know, so it can leave the row pending.
        """
        if not notifications:
            return

        body = {
            "eventId": str(event_id),
            "occurredAt": datetime.now(UTC).isoformat(),
            "source": self._source,
            "notifications": notifications,
        }

        message = aio_pika.Message(
            body=json.dumps(body).encode(),
            content_type="application/json",
            # Persistent, so an event the broker has accepted survives a restart of it.
            # The exchange and queue are durable for the same reason.
            delivery_mode=aio_pika.DeliveryMode.PERSISTENT,
            message_id=body["eventId"],
        )

        # Two attempts, because the first publish after a broker restart is expected to
        # fail: the cached connection looks alive until it is used, and only the attempt
        # itself reveals otherwise. The second attempt redials, so it is the one that
        # gets through.
        last: Exception | None = None
        for _ in range(2):
            try:
                await asyncio.wait_for(
                    self._publish_once(message, routing_key), timeout=_PUBLISH_TIMEOUT_SECONDS
                )
                return
            except Exception as exc:
                # The connection is dead; drop it so the next attempt redials rather
                # than failing again against the same stale channel.
                self._connection = None
                self._exchange = None
                last = exc
        raise last if last else RuntimeError("publish failed")


def enqueue(session: AsyncSession, routing_key: str, notifications: Sequence[dict]) -> None:
    """Adds an event to the caller's transaction.

    This is the whole point of the pattern: the event and the business change it
    describes are one write. There is no window in which an offer exists but the
    notification has vanished, because a failure rolls back both. The caller commits.
    """
    if not notifications:
        return
    session.add(
        NotificationOutbox(
            event_id=uuid.uuid4(),
            routing_key=routing_key,
            payload=list(notifications),
        )
    )


_RELAY_INTERVAL_SECONDS = 2.0
_RELAY_BATCH_SIZE = 100


class Relay:
    """Drains the outbox to the broker.

    It polls rather than listens: a LISTEN/NOTIFY subscription would be lower latency
    but would also silently miss rows written while the connection was down, which is
    the one case the outbox exists for. A poll cannot miss anything - it re-reads the
    table - and wake() covers the latency the interval would otherwise add.
    """

    def __init__(self, sessionmaker: async_sessionmaker[AsyncSession], publisher: Publisher):
        self._sessionmaker = sessionmaker
        self._publisher = publisher
        self._wake = asyncio.Event()
        self._task: asyncio.Task | None = None

    def start(self) -> None:
        self._task = asyncio.create_task(self._run(), name="outbox-relay")

    def wake(self) -> None:
        """Asks the relay to drain now instead of waiting for the next tick."""
        self._wake.set()

    async def aclose(self) -> None:
        if self._task is not None:
            self._task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await self._task
            self._task = None

    async def _run(self) -> None:
        while True:
            try:
                sent = await self._drain()
                if sent:
                    log.info("outbox relay published %d event(s)", sent)
            except asyncio.CancelledError:
                raise
            except Exception:
                log.exception("outbox relay failed; will retry")

            self._wake.clear()
            with contextlib.suppress(TimeoutError):
                await asyncio.wait_for(self._wake.wait(), timeout=_RELAY_INTERVAL_SECONDS)

    async def _drain(self) -> int:
        """Publishes one batch.

        Each row is marked sent in the same transaction that read it, and the read takes
        FOR UPDATE SKIP LOCKED, so two replicas cannot publish the same event.
        """
        async with self._sessionmaker() as session:
            rows = (
                await session.scalars(
                    select(NotificationOutbox)
                    .where(NotificationOutbox.published_at.is_(None))
                    .order_by(NotificationOutbox.created_at)
                    .limit(_RELAY_BATCH_SIZE)
                    .with_for_update(skip_locked=True)
                )
            ).all()
            if not rows:
                return 0

            published = 0
            for row in rows:
                try:
                    await self._publisher.publish_event(
                        row.event_id, row.routing_key, list(row.payload)
                    )
                except Exception as exc:
                    # The broker is unreachable. Leave the row pending and stop: the rows
                    # behind it would fail too, and the next tick retries from here.
                    await session.execute(
                        update(NotificationOutbox)
                        .where(NotificationOutbox.outbox_id == row.outbox_id)
                        .values(attempts=NotificationOutbox.attempts + 1, last_error=str(exc))
                    )
                    break

                await session.execute(
                    update(NotificationOutbox)
                    .where(NotificationOutbox.outbox_id == row.outbox_id)
                    .values(
                        published_at=datetime.now(UTC),
                        attempts=NotificationOutbox.attempts + 1,
                        last_error=None,
                    )
                )
                published += 1

            await session.commit()
            return published
