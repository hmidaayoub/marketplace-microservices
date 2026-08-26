"""Public API: /api/offers/** (spec section 11)."""

import uuid
from typing import Annotated, Any

from fastapi import APIRouter, Depends, Request, Response, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import service
from app.db import get_session
from app.models import Offer
from app.schemas import CompetingOfferOut, OfferCreate, OfferOut, OfferUpdate
from app.security import (
    ROLE_ADMIN,
    ROLE_CUSTOMER,
    ROLE_SELLER,
    CurrentPrincipal,
    Principal,
    require_role,
)
from app.service import REQUEST_STATES_CLOSED_TO_OFFERS, RequestNotAcceptingOffers

router = APIRouter(prefix="/api/offers", tags=["offers"])

SessionDep = Annotated[AsyncSession, Depends(get_session)]
SellerOnly = Annotated[Principal, Depends(require_role(ROLE_SELLER))]


def _clients(request: Request):
    return request.app.state.clients


@router.post("", status_code=status.HTTP_201_CREATED, response_model=None)
async def submit_offer(
    payload: OfferCreate, principal: SellerOnly, session: SessionDep, request: Request
) -> dict[str, Any]:
    """R5: only against an existing request. R6: always starts PENDING."""
    clients = _clients(request)

    seller_id = await clients.resolve_seller_id(principal.user_id)

    purchase_request = await clients.get_request(payload.request_id)
    if purchase_request.status in REQUEST_STATES_CLOSED_TO_OFFERS:
        raise RequestNotAcceptingOffers(purchase_request.status)

    # Resolved before the offer is written, because it is a network call: section 18
    # addresses NEW_OFFER to "Admin" rather than to one person, and notification-service
    # is addressed by userId and never resolves an identity of its own. An empty list -
    # auth-service unreachable, or no admin account - simply announces nothing.
    admin_ids = await clients.admin_user_ids()

    offer = await service.create_offer(session, seller_id, payload, admin_ids)

    # The event is already durable; this only saves it waiting for the next poll.
    request.app.state.relay.wake()

    return OfferOut.of(offer).model_dump(by_alias=True, mode="json")


@router.get("/me", response_model=None)
async def my_offers(
    principal: SellerOnly, session: SessionDep, request: Request
) -> list[dict[str, Any]]:
    seller_id = await _clients(request).resolve_seller_id(principal.user_id)
    offers = await service.list_for_seller(session, seller_id)
    return [OfferOut.of(offer).model_dump(by_alias=True, mode="json") for offer in offers]


@router.get("/request/{request_id}", response_model=None)
async def offers_for_request(
    request_id: uuid.UUID, principal: CurrentPrincipal, session: SessionDep, request: Request
) -> list[dict[str, Any]]:
    """Offers against one request.

    Customers and admins see them in full. A seller sees their own offer in full but
    only the anonymised view of a competitor's, so browsing the market cannot tell them
    who they are bidding against.
    """
    offers = await service.list_for_request(session, request_id)

    if principal.role in (ROLE_CUSTOMER, ROLE_ADMIN):
        return [OfferOut.of(offer).model_dump(by_alias=True, mode="json") for offer in offers]

    own_seller_id = await _clients(request).resolve_seller_id(principal.user_id)
    return [_project_for_seller(offer, own_seller_id) for offer in offers]


@router.get("/{offer_id}", response_model=None)
async def get_offer(
    offer_id: uuid.UUID, principal: CurrentPrincipal, session: SessionDep, request: Request
) -> dict[str, Any]:
    offer = await service.get_offer(session, offer_id)

    if principal.role in (ROLE_CUSTOMER, ROLE_ADMIN):
        return OfferOut.of(offer).model_dump(by_alias=True, mode="json")

    own_seller_id = await _clients(request).resolve_seller_id(principal.user_id)
    return _project_for_seller(offer, own_seller_id)


@router.put("/{offer_id}", response_model=None)
async def update_offer(
    offer_id: uuid.UUID,
    payload: OfferUpdate,
    principal: SellerOnly,
    session: SessionDep,
    request: Request,
) -> dict[str, Any]:
    seller_id = await _clients(request).resolve_seller_id(principal.user_id)
    offer = await service.update_offer(session, offer_id, seller_id, payload)
    return OfferOut.of(offer).model_dump(by_alias=True, mode="json")


@router.delete("/{offer_id}", status_code=status.HTTP_204_NO_CONTENT)
async def cancel_offer(
    offer_id: uuid.UUID, principal: SellerOnly, session: SessionDep, request: Request
) -> Response:
    seller_id = await _clients(request).resolve_seller_id(principal.user_id)
    await service.cancel_offer(session, offer_id, seller_id)
    return Response(status_code=status.HTTP_204_NO_CONTENT)


def _project_for_seller(offer: Offer, own_seller_id: uuid.UUID) -> dict[str, Any]:
    if offer.seller_id == own_seller_id:
        return OfferOut.of(offer).model_dump(by_alias=True, mode="json")
    return CompetingOfferOut.of(offer).model_dump(by_alias=True, mode="json")
