"""Event handling (see docs/events.md).

These drive the domain function the consumer calls rather than a live broker: what is
worth pinning down is the dedupe and the shared code path, and both are decided in the
database. The AMQP wiring itself is exercised end-to-end against a real RabbitMQ in the
running stack.
"""

import uuid

import pytest
from pydantic import ValidationError

from app.schemas import EventEnvelope
from app.service import DuplicateEvent, record_event
from tests.conftest import auth, make_token, notification_body

CUSTOMER = "CUSTOMER"


def envelope(*bodies, event_id: uuid.UUID | None = None, source: str = "request-service") -> dict:
    return {
        "eventId": str(event_id or uuid.uuid4()),
        "occurredAt": "2026-08-26T09:41:12.482000Z",
        "source": source,
        "notifications": list(bodies),
    }


async def session_for(client):
    """A session on the same engine the app uses."""
    from app.db import get_sessionmaker

    return get_sessionmaker()()


async def test_an_event_creates_its_notifications(client):
    user_id = uuid.uuid4()
    payload = EventEnvelope.model_validate(envelope(notification_body(user_id)))

    async with await session_for(client) as session:
        created = await record_event(session, payload)

    assert len(created) == 1
    assert created[0].status == "SENT"

    listing = await client.get("/api/notifications/me", headers=auth(make_token(user_id, CUSTOMER)))
    assert len(listing.json()) == 1


async def test_a_redelivered_event_is_not_applied_twice(client):
    """At-least-once delivery means the same event legitimately arrives again after a
    consumer dies before acking. It must not double the user's inbox."""
    user_id = uuid.uuid4()
    event_id = uuid.uuid4()
    payload = EventEnvelope.model_validate(envelope(notification_body(user_id), event_id=event_id))

    async with await session_for(client) as session:
        await record_event(session, payload)

    async with await session_for(client) as session:
        with pytest.raises(DuplicateEvent):
            await record_event(session, payload)

    listing = await client.get("/api/notifications/me", headers=auth(make_token(user_id, CUSTOMER)))
    assert len(listing.json()) == 1, "the redelivery duplicated the notification"


async def test_a_fan_out_event_is_one_transaction(client):
    """REQUEST_CLOSED to every participant: all of them, or none."""
    recipients = [uuid.uuid4() for _ in range(3)]
    payload = EventEnvelope.model_validate(
        envelope(*[notification_body(r, type="REQUEST_CLOSED") for r in recipients])
    )

    async with await session_for(client) as session:
        created = await record_event(session, payload)

    assert {n.user_id for n in created} == set(recipients)


@pytest.mark.parametrize(
    "broken",
    [
        {"eventId": "not-a-uuid"},
        {"source": ""},
        {"notifications": []},
        {"unexpectedField": "surprise"},
    ],
)
async def test_malformed_envelopes_are_rejected(broken):
    """The consumer dead-letters what this rejects, rather than requeueing a poison
    message that would be redelivered forever."""
    body = envelope(notification_body(uuid.uuid4()))
    body.update(broken)

    with pytest.raises(ValidationError):
        EventEnvelope.model_validate(body)


async def test_an_unknown_producer_field_is_not_silently_dropped():
    """A producer that adds a field the consumer does not understand should be a loud
    dead-letter, not a value quietly discarded."""
    body = envelope(notification_body(uuid.uuid4(), priority="URGENT"))

    with pytest.raises(ValidationError):
        EventEnvelope.model_validate(body)


async def test_the_event_path_and_the_http_path_agree(client):
    """Both entry points run the same code, so a notification made either way is
    indistinguishable."""
    via_event, via_http = uuid.uuid4(), uuid.uuid4()

    async with await session_for(client) as session:
        await record_event(
            session, EventEnvelope.model_validate(envelope(notification_body(via_event)))
        )

    from tests.conftest import internal_headers

    await client.post(
        "/internal/notifications", json=notification_body(via_http), headers=internal_headers()
    )

    inbox = "/api/notifications/me"
    a = (await client.get(inbox, headers=auth(make_token(via_event, CUSTOMER)))).json()
    b = (await client.get(inbox, headers=auth(make_token(via_http, CUSTOMER)))).json()

    ignore = {"notificationId", "userId", "createdAt", "sentAt"}
    assert {k: v for k, v in a[0].items() if k not in ignore} == {
        k: v for k, v in b[0].items() if k not in ignore
    }
