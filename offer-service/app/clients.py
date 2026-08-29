"""Outbound calls to the services that own the identities this one references."""

import logging
import uuid
from dataclasses import dataclass

import httpx

from app.security import INTERNAL_API_KEY_HEADER

log = logging.getLogger(__name__)


class SellerProfileMissing(Exception):
    """The authenticated user has no seller profile, so they cannot make an offer."""


class RequestMissing(Exception):
    """R5: an offer may only be submitted against a request that exists."""


class UpstreamUnavailable(Exception):
    """A dependency is unreachable or answered unexpectedly. Reported as 503."""


@dataclass(frozen=True)
class PurchaseRequest:
    request_id: uuid.UUID
    status: str
    total_customers: int
    total_quantity: int


class ServiceClients:
    """Wraps seller-service and request-service. One httpx client is shared for the
    process so connections are pooled rather than re-established per request."""

    def __init__(
        self,
        seller_base_url: str,
        request_base_url: str,
        auth_base_url: str,
        api_key: str,
        timeout: float,
    ):
        self._seller_base_url = seller_base_url.rstrip("/")
        self._request_base_url = request_base_url.rstrip("/")
        self._auth_base_url = auth_base_url.rstrip("/")
        self._client = httpx.AsyncClient(
            timeout=timeout, headers={INTERNAL_API_KEY_HEADER: api_key}
        )

    async def aclose(self) -> None:
        await self._client.aclose()

    async def admin_user_ids(self) -> list[uuid.UUID]:
        """Every ACTIVE admin, as userIds.

        Spec section 18 addresses NEW_OFFER to "Admin" rather than to one person, and
        roles live in auth-service. Notification-service is addressed by userId and
        never resolves an identity, so the producer has to build the recipient list.

        Returns an empty list rather than raising when auth-service is unreachable:
        this is only ever called to address a best-effort notification, and failing to
        find the admins must not fail the offer that was already stored.
        """
        url = f"{self._auth_base_url}/internal/users/by-role/ADMIN"
        try:
            response = await self._client.get(url)
            response.raise_for_status()
        except httpx.HTTPError as exc:
            log.error("cannot list admins to notify: %s", exc)
            return []

        try:
            return [uuid.UUID(user["userId"]) for user in response.json()]
        except (ValueError, KeyError, TypeError) as exc:
            log.error("unexpected admin listing from auth-service: %s", exc)
            return []

    async def resolve_seller_id(self, user_id: uuid.UUID) -> uuid.UUID:
        """Turn the token's userId into the sellerId an offer is recorded against.

        The identity model (spec section 5) keeps userId global and sellerId local to
        seller-service, so this service never accepts a sellerId from a caller.
        """
        url = f"{self._seller_base_url}/internal/sellers/by-user/{user_id}"
        try:
            response = await self._client.get(url)
        except httpx.HTTPError as exc:
            raise UpstreamUnavailable(f"seller-service: {exc}") from exc

        if response.status_code == 404:
            raise SellerProfileMissing
        if response.status_code != 200:
            raise UpstreamUnavailable(f"seller-service returned {response.status_code}")

        seller_id = response.json().get("sellerId")
        if not seller_id:
            raise UpstreamUnavailable("seller-service response had no sellerId")
        return uuid.UUID(seller_id)

    async def ensure_request_for_item(
        self, item_name: str, description: str, category: str
    ) -> PurchaseRequest:
        """The request an item is carried by, opening one with no buyers if it has none.

        This is the other half of R5. An offer still only exists against a request - what
        changes is that a seller no longer has to wait for one: naming the item is enough,
        and request-service opens it with nobody on it for buyers to join later.

        It is request-service that decides whether anything is created. Two requests for
        one item would split the total the platform exists to pool, so an item that
        already has a request - open, or emptied, or opened by another seller's offer -
        hands that one back and the offer joins the demand already there.
        """
        url = f"{self._request_base_url}/internal/requests"
        payload = {"itemName": item_name, "description": description, "category": category}
        try:
            response = await self._client.post(url, json=payload)
        except httpx.HTTPError as exc:
            raise UpstreamUnavailable(f"request-service: {exc}") from exc

        # 201 when it opened one, 200 when the item already had a request. Both carry the
        # request, and this caller wants the id either way.
        if response.status_code not in (200, 201):
            raise UpstreamUnavailable(f"request-service returned {response.status_code}")

        return _to_request(response.json())

    async def report_offer_count(self, request_id: uuid.UUID, total_offers: int) -> None:
        """Tell request-service how many live offers stand on a request.

        It is half of what a request's status means: one with no buyers but a standing
        offer is not dormant, and request-service cannot work that out for itself because
        the offers are this service's data.

        Best-effort, like admin_user_ids and for the same reason - the offer is already
        stored, and failing to describe it must not undo it. A failure costs a request
        the right status until the next offer on it is made, cancelled or decided, at
        which point this is called again with the whole count and the drift is gone. The
        count is absolute rather than a delta precisely so that recovery is automatic.
        """
        url = f"{self._request_base_url}/internal/requests/{request_id}/offers/count"
        try:
            response = await self._client.put(url, json={"totalOffers": total_offers})
            response.raise_for_status()
        except httpx.HTTPError as exc:
            log.error("cannot report the offer count for request %s: %s", request_id, exc)

    async def get_request(self, request_id: uuid.UUID) -> PurchaseRequest:
        url = f"{self._request_base_url}/internal/requests/{request_id}"
        try:
            response = await self._client.get(url)
        except httpx.HTTPError as exc:
            raise UpstreamUnavailable(f"request-service: {exc}") from exc

        if response.status_code == 404:
            raise RequestMissing
        if response.status_code != 200:
            raise UpstreamUnavailable(f"request-service returned {response.status_code}")

        return _to_request(response.json())


def _to_request(body: dict) -> PurchaseRequest:
    return PurchaseRequest(
        request_id=uuid.UUID(body["requestId"]),
        status=body["status"],
        total_customers=body.get("totalCustomers", 0),
        total_quantity=body.get("totalQuantity", 0),
    )
