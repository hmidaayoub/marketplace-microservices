"""Behaviour of the public and internal Offer API."""

import uuid

import pytest
from httpx import AsyncClient

from tests.conftest import FakeUpstream, auth, internal_headers, make_token


async def _seller(client: AsyncClient, upstream: FakeUpstream) -> tuple[uuid.UUID, uuid.UUID, str]:
    user_id, seller_id = uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    return user_id, seller_id, make_token(user_id, "SELLER")


async def _open_request(upstream: FakeUpstream, status: str = "OPEN") -> uuid.UUID:
    request_id = uuid.uuid4()
    upstream.add_request(request_id, status)
    return request_id


def _body(request_id: uuid.UUID, **overrides) -> dict:
    payload = {
        "requestId": str(request_id),
        "availableQuantity": 10,
        "pricePerUnit": "24.50",
        "currency": "EUR",
        "description": "Refurbished, 12 month warranty",
    }
    payload.update(overrides)
    return payload


# --- submitting (R5, R6) ----------------------------------------------------------


async def test_submit_offer_starts_pending(client: AsyncClient, upstream: FakeUpstream):
    _, seller_id, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)

    response = await client.post("/api/offers", json=_body(request_id), headers=auth(token))

    assert response.status_code == 201, response.text
    body = response.json()
    # R6: a new offer is always PENDING, whatever the caller wanted.
    assert body["status"] == "PENDING"
    assert body["sellerId"] == str(seller_id)
    assert body["requestId"] == str(request_id)
    assert body["pricePerUnit"] == "24.50"


async def test_submit_offer_requires_authentication(client: AsyncClient):
    response = await client.post("/api/offers", json=_body(uuid.uuid4()))
    assert response.status_code == 401


async def test_submit_offer_rejects_customer(client: AsyncClient, upstream: FakeUpstream):
    request_id = await _open_request(upstream)
    token = make_token(uuid.uuid4(), "CUSTOMER")

    response = await client.post("/api/offers", json=_body(request_id), headers=auth(token))

    assert response.status_code == 403
    assert "CUSTOMER" in response.json()["message"]


async def test_submit_offer_rejects_user_without_seller_profile(
    client: AsyncClient, upstream: FakeUpstream
):
    request_id = await _open_request(upstream)
    token = make_token(uuid.uuid4(), "SELLER")  # never registered with the stub

    response = await client.post("/api/offers", json=_body(request_id), headers=auth(token))

    assert response.status_code == 403
    assert "seller profile" in response.json()["message"].lower()


async def test_submit_offer_rejects_unknown_request(client: AsyncClient, upstream: FakeUpstream):
    """R5: an offer may only be submitted against a request that exists."""
    _, _, token = await _seller(client, upstream)

    response = await client.post("/api/offers", json=_body(uuid.uuid4()), headers=auth(token))

    assert response.status_code == 404
    assert "request" in response.json()["message"].lower()


@pytest.mark.parametrize("request_status", ["OPEN", "INACTIVE"])
async def test_submit_offer_accepted_whatever_the_request_status(
    client: AsyncClient, upstream: FakeUpstream, request_status: str
):
    """No status refuses an offer any more. A request is OPEN or INACTIVE, neither ends
    it, and one join revives an empty one - so refusing here only turns away a seller
    who was early."""
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream, request_status)

    response = await client.post("/api/offers", json=_body(request_id), headers=auth(token))

    assert response.status_code == 201


async def test_submit_offer_allowed_while_other_offers_pending(
    client: AsyncClient, upstream: FakeUpstream
):
    """Several sellers compete on the same demand, and an approval no longer ends it."""
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream, "OPEN")

    response = await client.post("/api/offers", json=_body(request_id), headers=auth(token))

    assert response.status_code == 201


async def test_submit_offer_sends_internal_api_key(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)

    await client.post("/api/offers", json=_body(request_id), headers=auth(token))

    assert upstream.seen_api_keys, "no upstream call was made"
    assert all(key == "test-internal-api-key" for key in upstream.seen_api_keys)


@pytest.mark.parametrize(
    "overrides",
    [
        {"availableQuantity": 0},
        {"availableQuantity": -3},
        {"pricePerUnit": "0"},
        {"pricePerUnit": "-1.00"},
        {"currency": "EURO"},
        {"currency": "12"},
    ],
)
async def test_submit_offer_rejects_invalid_body(
    client: AsyncClient, upstream: FakeUpstream, overrides: dict
):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers", json=_body(request_id, **overrides), headers=auth(token)
    )

    # Mapped to 400, not FastAPI's default 422, to match the other services.
    assert response.status_code == 400, response.text
    assert set(response.json()) == {"message", "status"}


async def test_submit_offer_rejects_caller_supplied_seller_id(
    client: AsyncClient, upstream: FakeUpstream
):
    """Identity comes from the token. A body claiming another seller is refused
    outright rather than quietly ignored."""
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers",
        json=_body(request_id, sellerId=str(uuid.uuid4())),
        headers=auth(token),
    )

    assert response.status_code == 400


async def test_submit_offer_rejects_caller_supplied_status(
    client: AsyncClient, upstream: FakeUpstream
):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers", json=_body(request_id, status="APPROVED"), headers=auth(token)
    )

    assert response.status_code == 400


# --- reading and projections ------------------------------------------------------


async def test_seller_sees_own_offer_in_full(client: AsyncClient, upstream: FakeUpstream):
    _, seller_id, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    offer_id = created.json()["offerId"]

    response = await client.get(f"/api/offers/{offer_id}", headers=auth(token))

    assert response.status_code == 200
    assert response.json()["sellerId"] == str(seller_id)


async def test_competing_seller_cannot_see_who_made_an_offer(
    client: AsyncClient, upstream: FakeUpstream
):
    """A seller may see the competitive terms but not the identity behind them."""
    _, _, first_token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(first_token))
    offer_id = created.json()["offerId"]

    _, _, rival_token = await _seller(client, upstream)
    response = await client.get(f"/api/offers/{offer_id}", headers=auth(rival_token))

    assert response.status_code == 200
    body = response.json()
    assert "sellerId" not in body
    assert body["pricePerUnit"] == "24.50"


async def test_customer_sees_offers_for_a_request_in_full(
    client: AsyncClient, upstream: FakeUpstream
):
    _, seller_id, seller_token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    await client.post("/api/offers", json=_body(request_id), headers=auth(seller_token))

    customer_token = make_token(uuid.uuid4(), "CUSTOMER")
    response = await client.get(f"/api/offers/request/{request_id}", headers=auth(customer_token))

    assert response.status_code == 200
    offers = response.json()
    assert len(offers) == 1
    assert offers[0]["sellerId"] == str(seller_id)


async def test_offers_for_request_hides_rival_identities_from_a_seller(
    client: AsyncClient, upstream: FakeUpstream
):
    _, mine_seller_id, my_token = await _seller(client, upstream)
    _, _, rival_token = await _seller(client, upstream)
    request_id = await _open_request(upstream)

    await client.post("/api/offers", json=_body(request_id), headers=auth(my_token))
    await client.post(
        "/api/offers", json=_body(request_id, pricePerUnit="19.99"), headers=auth(rival_token)
    )

    response = await client.get(f"/api/offers/request/{request_id}", headers=auth(my_token))

    assert response.status_code == 200
    offers = response.json()
    assert len(offers) == 2
    identified = [o for o in offers if "sellerId" in o]
    assert len(identified) == 1
    assert identified[0]["sellerId"] == str(mine_seller_id)


async def test_my_offers_lists_only_own(client: AsyncClient, upstream: FakeUpstream):
    _, _, my_token = await _seller(client, upstream)
    _, _, rival_token = await _seller(client, upstream)
    request_id = await _open_request(upstream)

    await client.post("/api/offers", json=_body(request_id), headers=auth(my_token))
    await client.post("/api/offers", json=_body(request_id), headers=auth(rival_token))

    response = await client.get("/api/offers/me", headers=auth(my_token))

    assert response.status_code == 200
    assert len(response.json()) == 1


async def test_get_offer_returns_404_for_unknown_id(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(client, upstream)
    response = await client.get(f"/api/offers/{uuid.uuid4()}", headers=auth(token))
    assert response.status_code == 404


async def test_malformed_offer_id_is_400_not_500(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(client, upstream)
    response = await client.get("/api/offers/not-a-uuid", headers=auth(token))
    assert response.status_code == 400


# --- updating and cancelling ------------------------------------------------------


async def test_update_own_pending_offer(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    offer_id = created.json()["offerId"]

    response = await client.put(
        f"/api/offers/{offer_id}",
        json={
            "availableQuantity": 4,
            "pricePerUnit": "31.00",
            "currency": "USD",
            "description": "revised",
        },
        headers=auth(token),
    )

    assert response.status_code == 200
    body = response.json()
    assert body["availableQuantity"] == 4
    assert body["pricePerUnit"] == "31.00"
    assert body["currency"] == "USD"
    assert body["status"] == "PENDING"


async def test_cannot_update_another_sellers_offer(client: AsyncClient, upstream: FakeUpstream):
    _, _, owner_token = await _seller(client, upstream)
    _, _, rival_token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(owner_token))
    offer_id = created.json()["offerId"]

    response = await client.put(
        f"/api/offers/{offer_id}",
        json={"availableQuantity": 1, "pricePerUnit": "1.00", "currency": "EUR"},
        headers=auth(rival_token),
    )

    assert response.status_code == 403


async def test_cancel_own_offer_sets_cancelled(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    offer_id = created.json()["offerId"]

    response = await client.delete(f"/api/offers/{offer_id}", headers=auth(token))
    assert response.status_code == 204

    # Cancelling is a status change, not a delete: the record survives for the audit trail.
    after = await client.get(f"/api/offers/{offer_id}", headers=auth(token))
    assert after.status_code == 200
    assert after.json()["status"] == "CANCELLED"


async def test_cannot_update_a_decided_offer(client: AsyncClient, upstream: FakeUpstream):
    """Once approved, the terms are frozen: contact permission may already have been
    granted against exactly what the admin saw."""
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    offer_id = created.json()["offerId"]

    approved = await client.patch(
        f"/internal/offers/{offer_id}/status",
        json={"status": "APPROVED"},
        headers=internal_headers(),
    )
    assert approved.status_code == 200

    response = await client.put(
        f"/api/offers/{offer_id}",
        json={"availableQuantity": 1, "pricePerUnit": "1.00", "currency": "EUR"},
        headers=auth(token),
    )
    assert response.status_code == 409


# --- internal API -----------------------------------------------------------------


async def test_internal_endpoints_require_the_api_key(client: AsyncClient, upstream: FakeUpstream):
    for response in (
        await client.get("/internal/offers/pending"),
        await client.get("/internal/offers/pending", headers=internal_headers("wrong-key")),
        await client.patch(f"/internal/offers/{uuid.uuid4()}/status", json={"status": "APPROVED"}),
    ):
        assert response.status_code == 401


async def test_internal_endpoints_reject_a_user_jwt(client: AsyncClient, upstream: FakeUpstream):
    """A user token is not an internal credential; the two call styles are separate."""
    _, _, token = await _seller(client, upstream)
    response = await client.get("/internal/offers/pending", headers=auth(token))
    assert response.status_code == 401


async def test_pending_queue_lists_only_pending(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)

    first = await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    await client.delete(f"/api/offers/{first.json()['offerId']}", headers=auth(token))

    response = await client.get("/internal/offers/pending", headers=internal_headers())

    assert response.status_code == 200
    statuses = [offer["status"] for offer in response.json()]
    assert statuses == ["PENDING"]


@pytest.mark.parametrize("decision", ["APPROVED", "REJECTED"])
async def test_internal_status_records_the_decision(
    client: AsyncClient, upstream: FakeUpstream, decision: str
):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    offer_id = created.json()["offerId"]

    response = await client.patch(
        f"/internal/offers/{offer_id}/status",
        json={"status": decision},
        headers=internal_headers(),
    )

    assert response.status_code == 200
    assert response.json()["status"] == decision


async def test_internal_status_rejects_a_second_decision(
    client: AsyncClient, upstream: FakeUpstream
):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    offer_id = created.json()["offerId"]

    await client.patch(
        f"/internal/offers/{offer_id}/status",
        json={"status": "APPROVED"},
        headers=internal_headers(),
    )
    second = await client.patch(
        f"/internal/offers/{offer_id}/status",
        json={"status": "REJECTED"},
        headers=internal_headers(),
    )

    assert second.status_code == 409


@pytest.mark.parametrize("bad_status", ["PENDING", "CANCELLED", "NONSENSE"])
async def test_internal_status_only_accepts_a_decision(
    client: AsyncClient, upstream: FakeUpstream, bad_status: str
):
    _, _, token = await _seller(client, upstream)
    request_id = await _open_request(upstream)
    created = await client.post("/api/offers", json=_body(request_id), headers=auth(token))
    offer_id = created.json()["offerId"]

    response = await client.patch(
        f"/internal/offers/{offer_id}/status",
        json={"status": bad_status},
        headers=internal_headers(),
    )

    assert response.status_code == 400


# --- health -----------------------------------------------------------------------


@pytest.mark.parametrize("path", ["/health", "/actuator/health"])
async def test_health_is_open(client: AsyncClient, path: str):
    response = await client.get(path)
    assert response.status_code == 200
    assert response.json()["status"] == "UP"
