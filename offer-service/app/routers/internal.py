"""Internal API: /internal/offers/** - service-to-service only, never routed by the
public gateway (spec section 6)."""

import uuid
from typing import Annotated, Any

from fastapi import APIRouter, Depends, Query
from sqlalchemy.ext.asyncio import AsyncSession

from app import service
from app.db import get_session
from app.schemas import OfferOut, StatusUpdate
from app.security import require_internal_api_key

router = APIRouter(
    prefix="/internal/offers",
    tags=["internal"],
    dependencies=[Depends(require_internal_api_key)],
)

SessionDep = Annotated[AsyncSession, Depends(get_session)]


@router.get("/pending", response_model=None)
async def pending_offers(
    session: SessionDep,
    limit: Annotated[int, Query(ge=1, le=100)] = 20,
    offset: Annotated[int, Query(ge=0)] = 0,
) -> list[dict[str, Any]]:
    """The admin review queue. Admin/Contact reads this to build its pending list."""
    offers = await service.list_pending(session, limit, offset)
    return [OfferOut.of(offer).model_dump(by_alias=True, mode="json") for offer in offers]


@router.get("/{offer_id}", response_model=None)
async def read_offer(offer_id: uuid.UUID, session: SessionDep) -> dict[str, Any]:
    """Reads one offer for a service that needs the terms behind it.

    Every other service exposes an internal read-by-id; this is offer-service's.
    Admin/Contact calls it before recording a decision, because the grant an approval
    produces links seller, customer, request and offer together, and it must take the
    sellerId and requestId from the service that owns them rather than from the admin
    submitting the decision.

    Declared after /pending so the literal path is matched first - otherwise "pending"
    would be parsed as an offer_id and rejected as a malformed UUID.
    """
    offer = await service.get_offer(session, offer_id)
    return OfferOut.of(offer).model_dump(by_alias=True, mode="json")


@router.patch("/{offer_id}/status", response_model=None)
async def set_status(
    offer_id: uuid.UUID, payload: StatusUpdate, session: SessionDep
) -> dict[str, Any]:
    """Records the outcome of an admin decision.

    R7 is enforced upstream: Admin/Contact authenticates the ADMIN, writes the
    OfferDecision that is the audit record, and then calls this. This service only
    holds the resulting status, and only accepts APPROVED or REJECTED on a PENDING offer.
    """
    offer = await service.set_status(session, offer_id, payload.status)
    return OfferOut.of(offer).model_dump(by_alias=True, mode="json")
