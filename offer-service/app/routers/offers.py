"""Public API: /api/offers/** (spec section 11)."""

import uuid
from typing import Annotated, Any

from fastapi import APIRouter, Depends, Request, Response, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import service
from app.clients import ServiceClients
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

router = APIRouter(prefix="/api/offers", tags=["offers"])

SessionDep = Annotated[AsyncSession, Depends(get_session)]
SellerOnly = Annotated[Principal, Depends(require_role(ROLE_SELLER))]


def _clients(request: Request) -> ServiceClients:
    return request.app.state.clients


@router.post(
    "",
    status_code=status.HTTP_201_CREATED,
    # response_model stays None because the handler projects by role; `responses`
    # documents the shape without FastAPI filtering the dict on the way out.
    response_model=None,
    responses={
        201: {"model": OfferOut},
        409: {
            "description": (
                "This seller already has a live offer on this request. The body carries it "
                "as `existing` - update that one with PUT /api/offers/{offerId}."
            )
        },
    },
)
async def submit_offer(
    payload: OfferCreate, principal: SellerOnly, session: SessionDep, request: Request
) -> dict[str, Any]:
    """R5: only against a request that exists. R6: always starts PENDING.

    The offer names either the request it answers or the item it is for. Both end at the
    same place - an offer stored against a request that exists - and they differ only in
    who had to have gone first.

    One live offer per seller per request: a seller who has already answered this demand
    is refused with their existing offer attached, to update rather than duplicate.
    """
    clients = _clients(request)

    seller_id = await clients.resolve_seller_id(principal.user_id)
    request_id = await _demand_answered(clients, payload)

    # Resolved before the offer is written, because it is a network call: section 18
    # addresses NEW_OFFER to "Admin" rather than to one person, and notification-service
    # is addressed by userId and never resolves an identity of its own. An empty list -
    # auth-service unreachable, or no admin account - simply announces nothing.
    admin_ids = await clients.admin_user_ids()

    offer = await service.create_offer(session, seller_id, request_id, payload, admin_ids)

    # The event is already durable; this only saves it waiting for the next poll.
    request.app.state.relay.wake()

    return OfferOut.of(offer).model_dump(by_alias=True, mode="json")


@router.get("/me", response_model=None, responses={200: {"model": list[OfferOut]}})
async def my_offers(
    principal: SellerOnly, session: SessionDep, request: Request
) -> list[dict[str, Any]]:
    seller_id = await _clients(request).resolve_seller_id(principal.user_id)
    offers = await service.list_for_seller(session, seller_id)
    return [OfferOut.of(offer).model_dump(by_alias=True, mode="json") for offer in offers]


@router.get(
    "/request/{request_id}",
    response_model=None,
    # Either shape, depending on who asks: a rival seller gets CompetingOfferOut with
    # the sellerId withheld, everyone else the full offer.
    responses={200: {"model": list[OfferOut | CompetingOfferOut]}},
)
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


@router.get(
    "/{offer_id}",
    response_model=None,
    responses={200: {"model": OfferOut | CompetingOfferOut}},
)
async def get_offer(
    offer_id: uuid.UUID, principal: CurrentPrincipal, session: SessionDep, request: Request
) -> dict[str, Any]:
    offer = await service.get_offer(session, offer_id)

    if principal.role in (ROLE_CUSTOMER, ROLE_ADMIN):
        return OfferOut.of(offer).model_dump(by_alias=True, mode="json")

    own_seller_id = await _clients(request).resolve_seller_id(principal.user_id)
    return _project_for_seller(offer, own_seller_id)


@router.put("/{offer_id}", response_model=None, responses={200: {"model": OfferOut}})
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


async def _demand_answered(clients: ServiceClients, payload: OfferCreate) -> uuid.UUID:
    """The request this offer is against, opening one for the item if it needs to.

    Given a requestId, the request only has to exist - that is R5 - and its status is not
    consulted. A request is OPEN or INACTIVE, neither is terminal, and an INACTIVE one is
    revived by a single join, so refusing an offer against it would only mean refusing a
    seller who is early.

    Given an item instead, there is no request to look up: nobody has asked for this
    product, and a seller with stock should not have to wait for someone to. So one is
    opened with no buyers on it, and the offer is what it carries until the first buyer
    joins. request-service decides whether that means creating anything - an item that
    already has a request hands back the one it has, so this can never split demand.
    """
    if payload.request_id is not None:
        await clients.get_request(payload.request_id)
        return payload.request_id

    item = payload.item
    opened = await clients.ensure_request_for_item(
        item.item_name, item.description, item.category
    )
    return opened.request_id


def _project_for_seller(offer: Offer, own_seller_id: uuid.UUID) -> dict[str, Any]:
    if offer.seller_id == own_seller_id:
        return OfferOut.of(offer).model_dump(by_alias=True, mode="json")
    return CompetingOfferOut.of(offer).model_dump(by_alias=True, mode="json")
