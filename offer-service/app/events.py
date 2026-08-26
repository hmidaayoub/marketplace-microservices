"""AMQP publisher for notification events (see docs/events.md).

Publishing is deliberately best-effort and always happens after the offer has been
stored. A broker that is down must not undo an offer that was genuinely submitted: the
offer exists, and only the "there is a new offer" message to the admins is missing.
Every failure here is logged and swallowed.
"""

import json
import logging
import uuid
from datetime import UTC, datetime

import aio_pika

log = logging.getLogger(__name__)

EXCHANGE = "marketplace.events"
DEAD_LETTER_EXCHANGE = "marketplace.events.dlx"
QUEUE = "notification.events"
DEAD_LETTER_QUEUE = "notification.events.dlq"
BINDING_KEY = "#"

KEY_OFFER_CREATED = "offer.created"

# Has to be set explicitly: a stopped broker on a Docker network blackholes the
# connection rather than refusing it, so an unbounded dial turns every offer submission
# into a long wait for a broker nobody is waiting on.
_DIAL_TIMEOUT_SECONDS = 0.7


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

    async def publish_or_log(self, routing_key: str, notifications: list[dict]) -> None:
        """Publishes one event. There is no error to ignore by accident: the
        best-effort contract is enforced by the signature."""
        if not notifications:
            return

        body = {
            "eventId": str(uuid.uuid4()),
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
        # itself reveals otherwise. Without the retry every broker blip silently costs
        # one notification. The second attempt redials, so it is the one that gets through.
        for attempt in range(2):
            try:
                exchange = await self._acquire()
                await exchange.publish(message, routing_key=routing_key)
                return
            except Exception:
                # The connection is dead; drop it so the next attempt redials rather
                # than failing again against the same stale channel.
                self._connection = None
                self._exchange = None
                if attempt == 1:
                    log.exception("publishing %s failed; continuing", routing_key)
