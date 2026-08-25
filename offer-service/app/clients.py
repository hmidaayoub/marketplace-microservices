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

    def __init__(self, seller_base_url: str, request_base_url: str, api_key: str, timeout: float):
        self._seller_base_url = seller_base_url.rstrip("/")
        self._request_base_url = request_base_url.rstrip("/")
        self._client = httpx.AsyncClient(
            timeout=timeout, headers={INTERNAL_API_KEY_HEADER: api_key}
        )

    async def aclose(self) -> None:
        await self._client.aclose()

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

        body = response.json()
        return PurchaseRequest(
            request_id=uuid.UUID(body["requestId"]),
            status=body["status"],
            total_customers=body.get("totalCustomers", 0),
            total_quantity=body.get("totalQuantity", 0),
        )
