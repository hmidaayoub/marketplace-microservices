"""A picture of what a seller is offering.

The rules that matter here are not about storage. They are that the format is read from
the bytes rather than believed from the upload, and that the picture obeys the same
projection as the offer it belongs to - a seller may not see a competitor's.
"""

import json
import uuid

from httpx import AsyncClient

from tests.conftest import FakeUpstream, auth, make_token

# A one-pixel PNG. Real bytes, because what the handler stores is decided by reading
# them - a placeholder would be refused, which is the point.
ONE_PIXEL_PNG = bytes.fromhex(
    "89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c489"
    "0000000a49444154789c63000100000500010d0a2db40000000049454e44ae426082"
)

JPEG = b"\xff\xd8\xff\xe0" + b"\x00" * 64
WEBP = b"RIFF\x00\x00\x00\x00WEBPVP8 " + b"\x00" * 64


async def _seller(upstream: FakeUpstream) -> tuple[uuid.UUID, uuid.UUID, str]:
    user_id, seller_id = uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    return user_id, seller_id, make_token(user_id, "SELLER")


async def _open_request(upstream: FakeUpstream) -> uuid.UUID:
    request_id = uuid.uuid4()
    upstream.add_request(request_id, "OPEN")
    return request_id


def _payload(request_id: uuid.UUID, **overrides) -> str:
    body = {
        "requestId": str(request_id),
        "availableQuantity": 10,
        "pricePerUnit": "24.50",
        "currency": "EUR",
        "description": "Refurbished, 12 month warranty",
    }
    body.update(overrides)
    return json.dumps(body)


def _multipart(payload: str, image: bytes | None, filename: str = "product.png") -> dict:
    """The body the browser form sends: the JSON in one part, the file beside it.

    Built by hand rather than through httpx's `files=`, because the no-image case is the
    one that matters most and httpx will not encode it: given form data and no file it
    falls back to application/x-www-form-urlencoded, which is not what a FormData with a
    single appended field produces in a browser. Writing the body means the test sends
    what the frontend actually sends.
    """
    boundary = "----offer-service-test-boundary"
    parts = [
        f"--{boundary}\r\n"
        f'Content-Disposition: form-data; name="payload"\r\n\r\n{payload}\r\n'.encode()
    ]
    if image is not None:
        parts.append(
            f"--{boundary}\r\n"
            f'Content-Disposition: form-data; name="image"; filename="{filename}"\r\n'
            f"Content-Type: image/png\r\n\r\n".encode()
            + image
            + b"\r\n"
        )
    parts.append(f"--{boundary}--\r\n".encode())

    return {
        "content": b"".join(parts),
        "headers": {"Content-Type": f"multipart/form-data; boundary={boundary}"},
    }


def _with_auth(token: str, body: dict) -> dict:
    """Merge the bearer header into a hand-built multipart body's own headers."""
    return {**body, "headers": {**body["headers"], **auth(token)}}


async def test_submit_offer_stores_and_serves_an_image(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers", **_with_auth(token, _multipart(_payload(request_id), ONE_PIXEL_PNG))
    )

    assert response.status_code == 201, response.text
    body = response.json()
    # The picture travels beside the offer without disturbing any of it.
    assert body["status"] == "PENDING"
    assert body["pricePerUnit"] == "24.50"
    assert body["hasImage"] is True

    image = await client.get(f"/api/offers/{body['offerId']}/image", headers=auth(token))
    assert image.status_code == 200, image.text
    assert image.headers["content-type"] == "image/png"
    assert image.headers["x-content-type-options"] == "nosniff"
    assert image.content == ONE_PIXEL_PNG


async def test_submit_offer_without_an_image_reports_none(
    client: AsyncClient, upstream: FakeUpstream
):
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers", **_with_auth(token, _multipart(_payload(request_id), None))
    )

    assert response.status_code == 201, response.text
    assert response.json()["hasImage"] is False

    image = await client.get(f"/api/offers/{response.json()['offerId']}/image", headers=auth(token))
    assert image.status_code == 404


async def test_plain_json_submission_still_works(client: AsyncClient, upstream: FakeUpstream):
    """The path every existing caller uses - the smoke script, the Postman collection,
    the Swagger UI. Adding images must not have moved it."""
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers", json=json.loads(_payload(request_id)), headers=auth(token)
    )

    assert response.status_code == 201, response.text
    assert response.json()["hasImage"] is False


async def test_a_file_that_is_not_an_image_is_refused(client: AsyncClient, upstream: FakeUpstream):
    """Storing markup and serving it back from the platform's own origin would be a
    stored XSS against every viewer. The filename and the declared content type both
    claim PNG; only the bytes are consulted."""
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers",
        **_with_auth(
            token, _multipart(_payload(request_id), b"<html><script>alert(1)</script></html>")
        ),
    )

    assert response.status_code == 400, response.text
    assert "unsupported image format" in response.json()["message"]


async def test_an_svg_is_refused(client: AsyncClient, upstream: FakeUpstream):
    """SVG is a document that can carry script, so it is not in the accepted set at all."""
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    svg = b'<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>'
    response = await client.post(
        "/api/offers", **_with_auth(token, _multipart(_payload(request_id), svg, "x.svg"))
    )

    assert response.status_code == 400, response.text


async def test_an_oversized_image_is_refused(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    # A real PNG header with more than the cap behind it, so it is the size that is
    # refused rather than the format.
    huge = ONE_PIXEL_PNG + b"\x00" * (1 << 20)
    response = await client.post(
        "/api/offers", **_with_auth(token, _multipart(_payload(request_id), huge))
    )

    assert response.status_code == 400, response.text


async def test_jpeg_and_webp_are_accepted(client: AsyncClient, upstream: FakeUpstream):
    for image, expected in ((JPEG, "image/jpeg"), (WEBP, "image/webp")):
        _, _, token = await _seller(upstream)
        request_id = await _open_request(upstream)

        response = await client.post(
            "/api/offers", **_with_auth(token, _multipart(_payload(request_id), image))
        )
        assert response.status_code == 201, response.text

        served = await client.get(
            f"/api/offers/{response.json()['offerId']}/image", headers=auth(token)
        )
        assert served.headers["content-type"] == expected


# --- who may see it ----------------------------------------------------------------


async def test_a_seller_cannot_see_a_competitors_image(client: AsyncClient, upstream: FakeUpstream):
    """The rule CompetingOfferOut exists for. A photograph is not anonymous - a shop
    sign or a watermark in frame names the seller as surely as sellerId would - so the
    picture of a rival's offer is not reachable, not merely unmentioned."""
    _, _, mine = await _seller(upstream)
    _, _, rivals = await _seller(upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers", **_with_auth(mine, _multipart(_payload(request_id), ONE_PIXEL_PNG))
    )
    assert response.status_code == 201, response.text
    offer_id = response.json()["offerId"]

    refused = await client.get(f"/api/offers/{offer_id}/image", headers=auth(rivals))
    assert refused.status_code == 404


async def test_a_customer_can_see_an_offers_image(client: AsyncClient, upstream: FakeUpstream):
    """A customer is who the offer is for, and already sees it in full."""
    _, _, seller = await _seller(upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers", **_with_auth(seller, _multipart(_payload(request_id), ONE_PIXEL_PNG))
    )
    assert response.status_code == 201, response.text

    customer = make_token(uuid.uuid4(), "CUSTOMER")
    served = await client.get(
        f"/api/offers/{response.json()['offerId']}/image", headers=auth(customer)
    )
    assert served.status_code == 200
    assert served.content == ONE_PIXEL_PNG


async def test_an_image_needs_a_token(client: AsyncClient, upstream: FakeUpstream):
    """Unlike a request's picture, which is public: an offer is not."""
    _, _, seller = await _seller(upstream)
    request_id = await _open_request(upstream)

    response = await client.post(
        "/api/offers", **_with_auth(seller, _multipart(_payload(request_id), ONE_PIXEL_PNG))
    )
    assert response.status_code == 201, response.text

    served = await client.get(f"/api/offers/{response.json()['offerId']}/image")
    assert served.status_code == 401


# --- changing it -------------------------------------------------------------------


async def test_updating_an_offer_replaces_its_image(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    created = await client.post(
        "/api/offers", **_with_auth(token, _multipart(_payload(request_id), ONE_PIXEL_PNG))
    )
    offer_id = created.json()["offerId"]

    update = json.dumps(
        {
            "availableQuantity": 5,
            "pricePerUnit": "19.99",
            "currency": "EUR",
            "description": "Now cheaper",
        }
    )
    changed = await client.put(
        f"/api/offers/{offer_id}", **_with_auth(token, _multipart(update, JPEG, "new.jpg"))
    )

    assert changed.status_code == 200, changed.text
    assert changed.json()["hasImage"] is True

    served = await client.get(f"/api/offers/{offer_id}/image", headers=auth(token))
    assert served.headers["content-type"] == "image/jpeg"
    assert served.content == JPEG


async def test_updating_terms_alone_keeps_the_existing_image(
    client: AsyncClient, upstream: FakeUpstream
):
    """A seller editing a price in a form that already shows their picture is not asking
    to delete it, so an absent image part leaves it alone."""
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    created = await client.post(
        "/api/offers", **_with_auth(token, _multipart(_payload(request_id), ONE_PIXEL_PNG))
    )
    offer_id = created.json()["offerId"]

    changed = await client.put(
        f"/api/offers/{offer_id}",
        headers=auth(token),
        json={
            "availableQuantity": 5,
            "pricePerUnit": "19.99",
            "currency": "EUR",
            "description": "Now cheaper",
        },
    )

    assert changed.status_code == 200, changed.text
    assert changed.json()["hasImage"] is True

    served = await client.get(f"/api/offers/{offer_id}/image", headers=auth(token))
    assert served.status_code == 200
    assert served.content == ONE_PIXEL_PNG


async def test_a_known_etag_is_answered_not_modified(client: AsyncClient, upstream: FakeUpstream):
    _, _, token = await _seller(upstream)
    request_id = await _open_request(upstream)

    created = await client.post(
        "/api/offers", **_with_auth(token, _multipart(_payload(request_id), ONE_PIXEL_PNG))
    )
    path = f"/api/offers/{created.json()['offerId']}/image"

    first = await client.get(path, headers=auth(token))
    etag = first.headers["etag"]

    second = await client.get(path, headers={**auth(token), "If-None-Match": etag})
    assert second.status_code == 304
    assert second.content == b""
