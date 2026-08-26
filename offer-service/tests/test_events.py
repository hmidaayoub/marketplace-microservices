"""NEW_OFFER publishing (spec flow 2 step 7, docs/events.md)."""

import uuid

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


async def test_a_new_offer_notifies_every_admin(client, upstream, publisher):
    """Section 18 addresses NEW_OFFER to "Admin" rather than one person, so the
    recipient list is built here - notification-service never resolves an identity."""
    user_id, seller_id, request_id = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    upstream.add_request(request_id)
    admins = [uuid.uuid4(), uuid.uuid4()]
    for admin in admins:
        upstream.add_admin(admin)

    response = await submit(client, make_token(user_id, SELLER), request_id)
    assert response.status_code == 201, response.text

    assert len(publisher.published) == 1
    routing_key, notifications = publisher.published[0]
    assert routing_key == "offer.created"
    assert [n["userId"] for n in notifications] == [str(a) for a in admins]
    assert {n["type"] for n in notifications} == {"NEW_OFFER"}
    # The admin is told what they are being asked to review.
    assert "149.99" in notifications[0]["message"]
    assert "EUR" in notifications[0]["message"]


async def test_no_admins_publishes_nothing(client, upstream, publisher):
    """An empty recipient list is not an event with no recipients."""
    user_id, seller_id, request_id = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    upstream.add_request(request_id)

    response = await submit(client, make_token(user_id, SELLER), request_id)
    assert response.status_code == 201

    assert publisher.published == []


async def test_a_rejected_offer_publishes_nothing(client, upstream, publisher):
    """The event must never announce an offer that was not stored."""
    user_id, seller_id = uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    upstream.add_admin(uuid.uuid4())
    token = make_token(user_id, SELLER)

    # R5: the request does not exist.
    unknown = await submit(client, token, uuid.uuid4())
    assert unknown.status_code == 404

    # And a request that is no longer accepting offers.
    closed_request = uuid.uuid4()
    upstream.add_request(closed_request, status="CLOSED")
    closed = await submit(client, token, closed_request)
    assert closed.status_code == 409

    assert publisher.published == []


async def test_updating_or_cancelling_an_offer_publishes_nothing(client, upstream, publisher):
    """Only submission is an event in section 18."""
    user_id, seller_id, request_id = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    upstream.add_request(request_id)
    upstream.add_admin(uuid.uuid4())
    token = make_token(user_id, SELLER)

    created = await submit(client, token, request_id)
    offer_id = created.json()["offerId"]
    published_after_create = len(publisher.published)

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

    assert len(publisher.published) == published_after_create


async def test_an_offer_survives_auth_service_being_unreachable(client, upstream, publisher):
    """Failing to find the admins must not fail the offer that was already stored."""
    user_id, seller_id, request_id = uuid.uuid4(), uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    upstream.add_request(request_id)

    # No admins registered and the stub answers 500 for anything it does not know,
    # which is what admin_user_ids() swallows.
    response = await submit(client, make_token(user_id, SELLER), request_id)

    assert response.status_code == 201
    assert publisher.published == []
