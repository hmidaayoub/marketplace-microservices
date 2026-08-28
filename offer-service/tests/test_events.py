"""NEW_OFFER, from the outbox to the broker (spec flow 2 step 7, docs/events.md)."""

import uuid

from sqlalchemy import select

from app.models import NotificationOutbox
from tests.conftest import auth, make_token

SELLER = "SELLER"


async def submit(client, token, request_id, **overrides):
    body = {
        "requestId": str(request_id),
        "availableQuantity": 6,
        "pricePerUnit": "149.99",
        "currency": "EUR",
        "description": "Sealed",
    }
    body.update(overrides)
    return await client.post("/api/offers", json=body, headers=auth(token))


async def outbox_rows():
    """What the transactions actually wrote, oldest first."""
    from app.db import get_sessionmaker

    async with get_sessionmaker()() as session:
        return (
            await session.scalars(
                select(NotificationOutbox).order_by(NotificationOutbox.created_at)
            )
        ).all()


async def a_seller(client, upstream, admins=0):
    user_id, seller_id, request_id = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    upstream.add_request(request_id)
    admin_ids = [uuid.uuid4() for _ in range(admins)]
    for admin in admin_ids:
        upstream.add_admin(admin)
    return make_token(user_id, SELLER), request_id, admin_ids


# --- what the transaction writes ------------------------------------------------------


async def test_a_new_offer_is_written_to_the_outbox_for_every_admin(client, upstream):
    """Section 18 addresses NEW_OFFER to "Admin" rather than one person, so the
    recipient list is built here - notification-service never resolves an identity."""
    token, request_id, admins = await a_seller(client, upstream, admins=2)

    response = await submit(client, token, request_id)
    assert response.status_code == 201, response.text

    rows = await outbox_rows()
    assert len(rows) == 1
    assert rows[0].routing_key == "offer.created"
    # Written, not yet sent: the relay owns publishing.
    assert rows[0].published_at is None
    assert rows[0].event_id is not None

    assert [n["userId"] for n in rows[0].payload] == [str(a) for a in admins]
    assert {n["type"] for n in rows[0].payload} == {"NEW_OFFER"}
    # The admin is told what they are being asked to review.
    assert "149.99" in rows[0].payload[0]["message"]
    assert "EUR" in rows[0].payload[0]["message"]


async def test_no_admins_writes_nothing(client, upstream):
    """An empty recipient list is not an event with no recipients."""
    token, request_id, _ = await a_seller(client, upstream, admins=0)

    assert (await submit(client, token, request_id)).status_code == 201
    assert await outbox_rows() == []


async def test_a_rejected_offer_writes_no_event(client, upstream):
    """The whole point of the outbox: the offer and the event are one write, so an
    offer that was never stored cannot leave an announcement behind."""
    token, _request_id, _ = await a_seller(client, upstream, admins=1)

    # R5, and now the only thing that refuses a submission: the request does not exist.
    assert (await submit(client, token, uuid.uuid4())).status_code == 404

    assert await outbox_rows() == []


async def test_updating_or_cancelling_an_offer_writes_no_event(client, upstream):
    """Only submission is an event in section 18."""
    token, request_id, _ = await a_seller(client, upstream, admins=1)

    created = await submit(client, token, request_id)
    offer_id = created.json()["offerId"]
    after_create = len(await outbox_rows())

    await client.put(
        f"/api/offers/{offer_id}",
        json={
            "availableQuantity": 9,
            "pricePerUnit": "10.00",
            "currency": "EUR",
            "description": "",
        },
        headers=auth(token),
    )
    await client.delete(f"/api/offers/{offer_id}", headers=auth(token))

    assert len(await outbox_rows()) == after_create


async def test_an_offer_survives_auth_service_being_unreachable(client, upstream):
    """Failing to find the admins must not fail the offer."""
    user_id, seller_id, request_id = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    upstream.add_request(request_id)
    # No admins registered; the stub answers 500 for anything it does not know, which is
    # what admin_user_ids() swallows.

    assert (await submit(client, make_token(user_id, SELLER), request_id)).status_code == 201
    assert await outbox_rows() == []


async def test_committing_an_offer_wakes_the_relay(client, upstream):
    """So the notification is not held for the poll interval when the broker is fine."""
    token, request_id, _ = await a_seller(client, upstream, admins=1)

    await submit(client, token, request_id)

    assert client.relay.wakes == 1


# --- what the relay does with it -------------------------------------------------------


async def test_the_relay_publishes_and_marks_the_row_sent(client, upstream, publisher):
    token, request_id, admins = await a_seller(client, upstream, admins=1)
    await submit(client, token, request_id)

    sent = await client.relay.drain()

    assert sent == 1
    assert len(publisher.published) == 1
    routing_key, notifications = publisher.published[0]
    assert routing_key == "offer.created"
    assert [n["userId"] for n in notifications] == [str(a) for a in admins]

    rows = await outbox_rows()
    assert rows[0].published_at is not None
    assert rows[0].attempts == 1
    assert rows[0].last_error is None


async def test_a_published_row_is_not_published_again(client, upstream, publisher):
    """The relay runs constantly; only unsent rows may be picked up."""
    token, request_id, _ = await a_seller(client, upstream, admins=1)
    await submit(client, token, request_id)

    assert await client.relay.drain() == 1
    assert await client.relay.drain() == 0
    assert len(publisher.published) == 1


async def test_the_published_event_id_is_the_one_the_transaction_recorded(
    client, upstream, publisher
):
    """The id is fixed at enqueue time so a relay that republishes a row after dying
    mid-commit sends the same id, which the consumer recognises as a redelivery."""
    token, request_id, _ = await a_seller(client, upstream, admins=1)
    await submit(client, token, request_id)

    stored = (await outbox_rows())[0].event_id

    captured: list = []
    original = publisher.publish_event

    async def capture(event_id, routing_key, notifications):
        captured.append(event_id)
        await original(event_id, routing_key, notifications)

    publisher.publish_event = capture
    await client.relay.drain()

    assert captured == [stored]


async def test_a_broker_failure_leaves_the_row_pending(client, upstream, publisher):
    """The event is durable, so an unreachable broker costs latency, not the
    notification. The row stays unsent for the next tick to retry."""
    token, request_id, _ = await a_seller(client, upstream, admins=1)
    await submit(client, token, request_id)

    async def fail(event_id, routing_key, notifications):
        raise RuntimeError("broker down")

    publisher.publish_event = fail
    assert await client.relay.drain() == 0

    rows = await outbox_rows()
    assert rows[0].published_at is None, "a failed publish must not mark the row sent"
    assert rows[0].attempts == 1
    assert "broker down" in rows[0].last_error

    # And once the broker is back, the same row goes out - nothing was lost.
    publisher.publish_event = RecordingReplacement(publisher).publish_event
    assert await client.relay.drain() == 1
    assert len(publisher.published) == 1


class RecordingReplacement:
    """Restores the recording behaviour after a test has swapped in a failing publish."""

    def __init__(self, publisher):
        self._publisher = publisher

    async def publish_event(self, event_id, routing_key, notifications):
        self._publisher.published.append((routing_key, notifications))
