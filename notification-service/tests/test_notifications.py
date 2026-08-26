"""Notification domain tests (spec sections 13 and 18)."""

import uuid

import pytest

from tests.conftest import auth, internal_headers, make_token, notification_body

CUSTOMER = "CUSTOMER"
SELLER = "SELLER"
ADMIN = "ADMIN"


async def create(client, user_id: uuid.UUID, **overrides):
    response = await client.post(
        "/internal/notifications",
        json=notification_body(user_id, **overrides),
        headers=internal_headers(),
    )
    assert response.status_code == 201, response.text
    return response.json()


# --- creating and delivering ---------------------------------------------------------


async def test_in_app_notification_is_delivered_by_being_stored(client):
    user_id = uuid.uuid4()
    body = await create(client, user_id)

    assert body["userId"] == str(user_id)
    assert body["type"] == "REQUEST_JOINED"
    assert body["channel"] == "IN_APP"
    # The row is the delivery, so there is nothing left to dispatch.
    assert body["status"] == "SENT"
    assert body["sentAt"] is not None


@pytest.mark.parametrize("channel", ["EMAIL", "SMS", "PUSH"])
async def test_channels_without_a_provider_stay_pending(client, channel):
    """Nothing has actually been sent, so the status must not claim otherwise."""
    body = await create(client, uuid.uuid4(), channel=channel)

    assert body["channel"] == channel
    assert body["status"] == "PENDING"
    assert body["sentAt"] is None


async def test_caller_cannot_choose_the_lifecycle(client):
    """status and notificationId are not fields on the create schema, so smuggling one
    in is rejected outright rather than silently ignored."""
    for field, value in (("status", "READ"), ("notificationId", str(uuid.uuid4()))):
        response = await client.post(
            "/internal/notifications",
            json=notification_body(uuid.uuid4(), **{field: value}),
            headers=internal_headers(),
        )
        assert response.status_code == 400, f"{field}: {response.text}"


@pytest.mark.parametrize(
    "overrides",
    [
        {"type": "lowercase_event"},
        {"type": "Has Spaces"},
        {"type": ""},
        {"title": ""},
        {"title": "   "},
        {"message": ""},
        {"channel": "CARRIER_PIGEON"},
        {"userId": "not-a-uuid"},
    ],
)
async def test_invalid_payloads_are_rejected(client, overrides):
    response = await client.post(
        "/internal/notifications",
        json=notification_body(uuid.uuid4(), **overrides),
        headers=internal_headers(),
    )
    assert response.status_code == 400, response.text


async def test_event_type_set_is_open(client):
    """Spec section 13 introduces the event list with "Examples", so an event the spec
    does not name must not require a migration here."""
    body = await create(client, uuid.uuid4(), type="SOME_FUTURE_EVENT_7")
    assert body["type"] == "SOME_FUTURE_EVENT_7"


@pytest.mark.parametrize(
    "event_type",
    [
        "REQUEST_JOINED",
        "NEW_OFFER",
        "OFFER_APPROVED",
        "OFFER_REJECTED",
        "CONTACT_ACCESS_GRANTED",
        "REQUEST_CLOSED",
    ],
)
async def test_every_event_in_the_spec_is_accepted(client, event_type):
    body = await create(client, uuid.uuid4(), type=event_type)
    assert body["status"] == "SENT"


# --- bulk fan-out ---------------------------------------------------------------------


async def test_bulk_creates_one_notification_per_recipient(client):
    """REQUEST_CLOSED goes to every participant of a request (spec section 18)."""
    recipients = [uuid.uuid4() for _ in range(3)]
    response = await client.post(
        "/internal/notifications/bulk",
        json={
            "notifications": [
                notification_body(user_id, type="REQUEST_CLOSED", title="Request closed")
                for user_id in recipients
            ]
        },
        headers=internal_headers(),
    )

    assert response.status_code == 201, response.text
    body = response.json()
    assert body["created"] == 3
    assert {n["userId"] for n in body["notifications"]} == {str(r) for r in recipients}
    assert all(n["status"] == "SENT" for n in body["notifications"])


async def test_bulk_is_all_or_nothing(client):
    """A half-delivered fan-out cannot be safely retried, so one bad entry rejects the
    whole call and writes nothing."""
    good, bad = uuid.uuid4(), uuid.uuid4()
    response = await client.post(
        "/internal/notifications/bulk",
        json={
            "notifications": [
                notification_body(good),
                notification_body(bad, type="not a valid type"),
            ]
        },
        headers=internal_headers(),
    )
    assert response.status_code == 400, response.text

    # The valid entry must not have been written.
    listing = await client.get("/api/notifications/me", headers=auth(make_token(good, CUSTOMER)))
    assert listing.json() == []


async def test_bulk_rejects_an_empty_or_oversized_batch(client):
    empty = await client.post(
        "/internal/notifications/bulk", json={"notifications": []}, headers=internal_headers()
    )
    assert empty.status_code == 400

    oversized = await client.post(
        "/internal/notifications/bulk",
        json={"notifications": [notification_body(uuid.uuid4()) for _ in range(501)]},
        headers=internal_headers(),
    )
    assert oversized.status_code == 400


# --- the inbox -------------------------------------------------------------------------


async def test_inbox_is_scoped_to_the_caller(client):
    mine, theirs = uuid.uuid4(), uuid.uuid4()
    await create(client, mine, title="Mine")
    await create(client, theirs, title="Theirs")

    response = await client.get("/api/notifications/me", headers=auth(make_token(mine, CUSTOMER)))

    assert response.status_code == 200
    body = response.json()
    assert len(body) == 1
    assert body[0]["title"] == "Mine"


async def test_inbox_is_newest_first_and_paginates(client):
    user_id = uuid.uuid4()
    for i in range(5):
        await create(client, user_id, title=f"Notification {i}")

    everything = await client.get(
        "/api/notifications/me", headers=auth(make_token(user_id, CUSTOMER))
    )
    titles = [n["title"] for n in everything.json()]
    assert titles == [f"Notification {i}" for i in reversed(range(5))]

    page = await client.get(
        "/api/notifications/me?limit=2&offset=1", headers=auth(make_token(user_id, CUSTOMER))
    )
    assert [n["title"] for n in page.json()] == ["Notification 3", "Notification 2"]


async def test_unread_filter_and_count(client):
    user_id = uuid.uuid4()
    token = make_token(user_id, CUSTOMER)
    first = await create(client, user_id, title="First")
    await create(client, user_id, title="Second")

    count = await client.get("/api/notifications/me/unread-count", headers=auth(token))
    assert count.json()["unreadCount"] == 2

    marked = await client.patch(
        f"/api/notifications/{first['notificationId']}/read", headers=auth(token)
    )
    assert marked.status_code == 200
    assert marked.json()["status"] == "READ"

    count = await client.get("/api/notifications/me/unread-count", headers=auth(token))
    assert count.json()["unreadCount"] == 1

    unread = await client.get("/api/notifications/me?unreadOnly=true", headers=auth(token))
    assert [n["title"] for n in unread.json()] == ["Second"]

    # The read one is still in the inbox, just no longer unread.
    everything = await client.get("/api/notifications/me", headers=auth(token))
    assert len(everything.json()) == 2


async def test_undelivered_notifications_do_not_count_as_unread(client):
    """A PENDING email is awaiting dispatch, not awaiting the user."""
    user_id = uuid.uuid4()
    await create(client, user_id, channel="EMAIL")

    count = await client.get(
        "/api/notifications/me/unread-count", headers=auth(make_token(user_id, CUSTOMER))
    )
    # PENDING counts as unread; it is a real message that has simply not gone out yet.
    assert count.json()["unreadCount"] == 1


async def test_pagination_bounds_are_validated(client):
    token = auth(make_token(uuid.uuid4(), CUSTOMER))
    for query in ("?limit=0", "?limit=101", "?offset=-1"):
        response = await client.get(f"/api/notifications/me{query}", headers=token)
        assert response.status_code == 400, f"{query}: {response.text}"


# --- marking read -----------------------------------------------------------------------


async def test_marking_read_is_idempotent(client):
    """Reading is not a decision: two devices syncing the same inbox is ordinary, so a
    repeat returns the notification unchanged rather than a 409."""
    user_id = uuid.uuid4()
    token = auth(make_token(user_id, CUSTOMER))
    created = await create(client, user_id)

    path = f"/api/notifications/{created['notificationId']}/read"
    first = await client.patch(path, headers=token)
    second = await client.patch(path, headers=token)

    assert first.status_code == 200
    assert second.status_code == 200
    assert second.json()["status"] == "READ"
    assert first.json()["sentAt"] == second.json()["sentAt"]


async def test_undelivered_notification_cannot_be_marked_read(client):
    """Nothing reached the user, so calling it read would be a lie."""
    user_id = uuid.uuid4()
    created = await create(client, user_id, channel="SMS")

    response = await client.patch(
        f"/api/notifications/{created['notificationId']}/read",
        headers=auth(make_token(user_id, CUSTOMER)),
    )
    assert response.status_code == 200
    assert response.json()["status"] == "PENDING"


async def test_another_users_notification_is_404_not_403(client):
    """403 would confirm the id exists, turning this route into an oracle for
    enumerating other people's notifications."""
    owner, stranger = uuid.uuid4(), uuid.uuid4()
    created = await create(client, owner)

    response = await client.patch(
        f"/api/notifications/{created['notificationId']}/read",
        headers=auth(make_token(stranger, CUSTOMER)),
    )
    assert response.status_code == 404

    # And it is genuinely untouched.
    still_unread = await client.get(
        "/api/notifications/me/unread-count", headers=auth(make_token(owner, CUSTOMER))
    )
    assert still_unread.json()["unreadCount"] == 1


async def test_unknown_notification_is_404(client):
    response = await client.patch(
        f"/api/notifications/{uuid.uuid4()}/read",
        headers=auth(make_token(uuid.uuid4(), CUSTOMER)),
    )
    assert response.status_code == 404


async def test_malformed_notification_id_is_400(client):
    """Issue #28: the same wording as every other service in the platform."""
    response = await client.patch(
        "/api/notifications/not-a-uuid/read", headers=auth(make_token(uuid.uuid4(), CUSTOMER))
    )
    assert response.status_code == 400
    assert response.json()["message"] == "Invalid value for parameter 'notificationId'"


# --- the security boundary ----------------------------------------------------------------


async def test_public_routes_require_a_token(client):
    created = await create(client, uuid.uuid4())
    for method, path in (
        ("get", "/api/notifications/me"),
        ("get", "/api/notifications/me/unread-count"),
        ("patch", f"/api/notifications/{created['notificationId']}/read"),
    ):
        response = await getattr(client, method)(path)
        assert response.status_code == 401, f"{path}: {response.text}"


@pytest.mark.parametrize("role", [CUSTOMER, SELLER, ADMIN])
async def test_every_role_has_an_inbox(client, role):
    """Notifications are addressed to a userId, not to a role: sellers get
    OFFER_APPROVED, admins get NEW_OFFER, customers get REQUEST_JOINED."""
    user_id = uuid.uuid4()
    await create(client, user_id)

    response = await client.get("/api/notifications/me", headers=auth(make_token(user_id, role)))
    assert response.status_code == 200
    assert len(response.json()) == 1


async def test_internal_routes_require_the_shared_key(client):
    body = notification_body(uuid.uuid4())

    no_key = await client.post("/internal/notifications", json=body)
    assert no_key.status_code == 401

    wrong_key = await client.post(
        "/internal/notifications", json=body, headers=internal_headers("wrong-key")
    )
    assert wrong_key.status_code == 401

    # A user JWT is not a substitute for the internal key.
    with_jwt = await client.post(
        "/internal/notifications", json=body, headers=auth(make_token(uuid.uuid4(), ADMIN))
    )
    assert with_jwt.status_code == 401

    bulk_no_key = await client.post("/internal/notifications/bulk", json={"notifications": [body]})
    assert bulk_no_key.status_code == 401


async def test_a_user_cannot_create_their_own_notification(client):
    """Creating is service-to-service only; there is no public write route at all."""
    user_id = uuid.uuid4()
    response = await client.post(
        "/api/notifications",
        json=notification_body(user_id),
        headers=auth(make_token(user_id, CUSTOMER)),
    )
    assert response.status_code in (404, 405)


async def test_health_reports_up(client):
    for path in ("/health", "/actuator/health"):
        response = await client.get(path)
        assert response.status_code == 200
        assert response.json()["status"] == "UP"
