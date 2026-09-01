"""Public API: /api/offers/** (spec section 11)."""

import uuid
from typing import Annotated, Any

from fastapi import APIRouter, Depends, Request, Response, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import media, service
from app.bodies import BadBody, json_or_multipart
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
from app.service import OfferNotFound

router = APIRouter(prefix="/api/offers", tags=["offers"])


def _body_doc(model_name: str, description: str) -> dict[str, Any]:
    """The requestBody these two operations take.

    Written by hand because the handlers read the body themselves - they accept JSON or
    multipart, and FastAPI documents one shape per parameter. Both content types are
    declared so the Swagger UI offers a file picker as well as a JSON box, rather than
    describing only the half a signature could express.
    """
    schema = {"$ref": f"#/components/schemas/{model_name}"}
    return {
        "requestBody": {
            "required": True,
            "description": description,
            "content": {
                "application/json": {"schema": schema},
                "multipart/form-data": {
                    "schema": {
                        "type": "object",
                        "properties": {
                            "payload": {
                                "type": "string",
                                "description": f"The {model_name} fields, as a JSON string.",
                            },
                            "image": {
                                "type": "string",
                                "format": "binary",
                                "description": (
                                    "Optional picture of the product. JPEG, PNG or WebP, "
                                    "at most 1 MiB. The format is read from the bytes, "
                                    "not from the part's declared content type."
                                ),
                            },
                        },
                        "required": ["payload"],
                    }
                },
            },
        }
    }


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
    openapi_extra=_body_doc("OfferCreate", "The offer, and optionally a picture of it."),
)
async def submit_offer(
    principal: SellerOnly, session: SessionDep, request: Request
) -> dict[str, Any]:
    """R5: only against a request that exists. R6: always starts PENDING.

    The offer names either the request it answers or the item it is for. Both end at the
    same place - an offer stored against a request that exists - and they differ only in
    who had to have gone first.

    One live offer per seller per request: a seller who has already answered this demand
    is refused with their existing offer attached, to update rather than duplicate.

    A picture of what is being offered is optional. Send the body as multipart/form-data
    with the JSON in a "payload" part and the file in an "image" part; a plain
    application/json body works exactly as it always has.
    """
    payload, image, image_type = await _body_with_image(request, OfferCreate)
    clients = _clients(request)

    seller_id = await clients.resolve_seller_id(principal.user_id)
    request_id = await _demand_answered(clients, payload)

    # Resolved before the offer is written, because it is a network call: section 18
    # addresses NEW_OFFER to "Admin" rather than to one person, and notification-service
    # is addressed by userId and never resolves an identity of its own. An empty list -
    # auth-service unreachable, or no admin account - simply announces nothing.
    admin_ids = await clients.admin_user_ids()

    offer = await service.create_offer(
        session, seller_id, request_id, payload, admin_ids, image, image_type
    )

    # The request's status depends on this: one with no buyers but a standing offer is
    # not dormant, and request-service cannot count offers it cannot see.
    await clients.report_offer_count(
        request_id, await service.count_live_offers(session, request_id)
    )

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


@router.put(
    "/{offer_id}",
    response_model=None,
    responses={200: {"model": OfferOut}},
    openapi_extra=_body_doc("OfferUpdate", "The new terms, and optionally a new picture."),
)
async def update_offer(
    offer_id: uuid.UUID,
    principal: SellerOnly,
    session: SessionDep,
    request: Request,
) -> dict[str, Any]:
    """Changing the terms of an offer already made. Only a PENDING offer accepts this.

    A picture sent here replaces the one the offer carries. Sending none leaves it
    alone: the seller is editing a price in a form that already shows their picture, and
    reading its absence as a delete would lose it by accident.
    """
    payload, image, image_type = await _body_with_image(request, OfferUpdate)
    seller_id = await _clients(request).resolve_seller_id(principal.user_id)
    offer = await service.update_offer(session, offer_id, seller_id, payload, image, image_type)
    return OfferOut.of(offer).model_dump(by_alias=True, mode="json")


@router.get(
    "/{offer_id}/image",
    response_class=Response,
    responses={
        200: {"content": {"image/*": {}}, "description": "The picture of what is offered"},
        404: {"description": "No such offer, or it carries no picture"},
    },
)
async def offer_image(
    offer_id: uuid.UUID, principal: CurrentPrincipal, session: SessionDep, request: Request
) -> Response:
    """The picture attached to an offer.

    Behind a token, unlike a request's picture, and behind the same projection that
    governs the offer itself: customers and admins see it, a seller sees their own but
    never a competitor's.

    That last rule is the point. CompetingOfferOut withholds sellerId so browsing the
    market cannot tell a seller who they are bidding against, and a photograph of a
    product is not anonymous - a shop sign, a watermark or a business card in frame
    names the seller as surely as the field does. An image endpoint any seller could
    read would be a way around the projection rather than a view of it, so the picture
    of a rival's offer is not served at all: it is not mentioned in their projection,
    and asking for it directly is a 404 like any other URL that holds nothing.
    """
    if principal.role == ROLE_SELLER:
        own_seller_id = await _clients(request).resolve_seller_id(principal.user_id)
        offer = await service.get_offer(session, offer_id)
        if offer.seller_id != own_seller_id:
            raise OfferNotFound

    data, image_type, updated_at = await service.offer_image(session, offer_id)

    # The stored timestamp rather than a hash of the bytes: a picture is replaced as a
    # whole or not at all, so the two change together, and hashing a megabyte on every
    # render to learn what a column already says would be work for nothing.
    etag = f'"{updated_at.timestamp()}"'
    headers = {
        "ETag": etag,
        # Private: the URL is per offer, and a shared cache holding it would serve one
        # viewer's image from another's edge.
        "Cache-Control": "private, max-age=300",
        # The browser is told what this file is and told not to look for anything else.
        # Without nosniff, a file that passed media.detect here could still be
        # re-sniffed as something scriptable by the browser.
        "X-Content-Type-Options": "nosniff",
    }

    if request.headers.get("if-none-match") == etag:
        return Response(status_code=status.HTTP_304_NOT_MODIFIED, headers=headers)

    return Response(content=data, media_type=image_type, headers=headers)


async def _body_with_image(
    request: Request, model: type[OfferCreate] | type[OfferUpdate]
) -> tuple[Any, bytes | None, str]:
    """The body, plus any picture sent beside it and the type the bytes actually are.

    The declared content type of the part is never consulted - see media.detect. A file
    that is not an image is refused here, before anything is written.
    """
    payload, image = await json_or_multipart(request, model)
    if not image:
        return payload, None, ""
    try:
        return payload, image, media.detect(image)
    except media.UnsupportedImage as exc:
        raise BadBody(str(exc)) from exc


@router.delete("/{offer_id}", status_code=status.HTTP_204_NO_CONTENT)
async def cancel_offer(
    offer_id: uuid.UUID, principal: SellerOnly, session: SessionDep, request: Request
) -> Response:
    clients = _clients(request)
    seller_id = await clients.resolve_seller_id(principal.user_id)
    cancelled = await service.cancel_offer(session, offer_id, seller_id)

    # Withdrawing the last live offer is what can let a request with no buyers go dormant.
    await clients.report_offer_count(
        cancelled.request_id, await service.count_live_offers(session, cancelled.request_id)
    )
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
    opened = await clients.ensure_request_for_item(item.item_name, item.description, item.category)
    return opened.request_id


def _project_for_seller(offer: Offer, own_seller_id: uuid.UUID) -> dict[str, Any]:
    if offer.seller_id == own_seller_id:
        return OfferOut.of(offer).model_dump(by_alias=True, mode="json")
    return CompetingOfferOut.of(offer).model_dump(by_alias=True, mode="json")
