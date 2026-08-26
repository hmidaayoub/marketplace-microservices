"""AMQP consumer for the notification events (see docs/events.md).

The service's inbound path in production. The HTTP internal API of spec section 13 is
still served and shares the same domain function, so the two cannot drift; what the
broker adds is that a producer never waits on this service and a notification is not
lost the moment it is down.
"""

import asyncio
import contextlib
import logging

import aio_pika
from aio_pika.abc import AbstractIncomingMessage, AbstractRobustConnection
from pydantic import ValidationError

from app import service
from app.db import get_sessionmaker
from app.schemas import EventEnvelope

log = logging.getLogger(__name__)

EXCHANGE = "marketplace.events"
DEAD_LETTER_EXCHANGE = "marketplace.events.dlx"
QUEUE = "notification.events"
DEAD_LETTER_QUEUE = "notification.events.dlq"

# The queue binds every key rather than a list. Spec section 13 introduces its event
# list with "Examples", so a new event must reach the consumer without a binding change
# - the same reason the type column is shape-checked instead of enumerated.
BINDING_KEY = "#"

# One message at a time per consumer. Each one is a short transaction, and a small
# prefetch keeps a restart from having to redeliver a large in-flight batch.
PREFETCH = 16

_RECONNECT_SECONDS = 5


class Consumer:
    """Owns the connection and the consume loop.

    Startup never blocks on the broker and never fails because of it. A service that
    refuses to start when RabbitMQ is slow would also stop serving the inbox, which is
    a plain database read that has nothing to do with messaging.
    """

    def __init__(self, url: str) -> None:
        self._url = url
        self._connection: AbstractRobustConnection | None = None
        self._task: asyncio.Task | None = None

    def start(self) -> None:
        self._task = asyncio.create_task(self._run(), name="notification-consumer")

    async def stop(self) -> None:
        if self._task is not None:
            self._task.cancel()
            with contextlib.suppress(asyncio.CancelledError):
                await self._task
            self._task = None
        if self._connection is not None:
            await self._connection.close()
            self._connection = None

    async def _run(self) -> None:
        while True:
            try:
                await self._consume()
            except asyncio.CancelledError:
                raise
            except Exception:
                log.exception("consumer connection lost; retrying in %ss", _RECONNECT_SECONDS)
                await asyncio.sleep(_RECONNECT_SECONDS)

    async def _consume(self) -> None:
        # connect_robust reconnects on its own; the loop above covers the case where
        # the broker is not reachable at all yet, which is normal on a cold start.
        self._connection = await aio_pika.connect_robust(self._url)

        async with self._connection:
            channel = await self._connection.channel()
            await channel.set_qos(prefetch_count=PREFETCH)
            await declare_topology(channel)

            queue = await channel.get_queue(QUEUE)
            log.info("consuming %s bound to %s with key %r", QUEUE, EXCHANGE, BINDING_KEY)

            await queue.consume(self._handle)
            await asyncio.Future()  # run until cancelled or the connection drops

    async def _handle(self, message: AbstractIncomingMessage) -> None:
        try:
            envelope = EventEnvelope.model_validate_json(message.body)
        except ValidationError as exc:
            # Unprocessable no matter how often it is retried. Requeueing it in place
            # would redeliver it immediately and forever, taking the consumer with it,
            # so it goes to the dead-letter queue for inspection instead.
            log.error("dead-lettering malformed event: %s", exc)
            await message.reject(requeue=False)
            return

        sessionmaker = get_sessionmaker()
        try:
            async with sessionmaker() as session:
                created = await service.record_event(session, envelope)
        except service.DuplicateEvent:
            # The expected shape of at-least-once delivery, not a problem: the event was
            # already applied, so acking is exactly right.
            log.info("event %s already handled; acking", envelope.event_id)
            await message.ack()
            return
        except Exception:
            log.exception("dead-lettering event %s after a failure", envelope.event_id)
            await message.reject(requeue=False)
            return

        log.info(
            "event %s from %s created %d notification(s)",
            envelope.event_id,
            envelope.source,
            len(created),
        )
        await message.ack()


async def declare_topology(channel: aio_pika.abc.AbstractChannel) -> None:
    """Declares the exchange, queue and bindings.

    Done by the service rather than by an operator or a compose file, so a fresh broker
    needs no setup step. Every declaration is idempotent and every side declares the
    same thing, so it does not matter which service connects first.
    """
    dead_letter_exchange = await channel.declare_exchange(
        DEAD_LETTER_EXCHANGE, aio_pika.ExchangeType.FANOUT, durable=True
    )
    dead_letter_queue = await channel.declare_queue(DEAD_LETTER_QUEUE, durable=True)
    await dead_letter_queue.bind(dead_letter_exchange)

    exchange = await channel.declare_exchange(EXCHANGE, aio_pika.ExchangeType.TOPIC, durable=True)
    queue = await channel.declare_queue(
        QUEUE,
        durable=True,
        arguments={"x-dead-letter-exchange": DEAD_LETTER_EXCHANGE},
    )
    await queue.bind(exchange, routing_key=BINDING_KEY)
