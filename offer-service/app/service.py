"""Offer domain logic (spec section 11)."""

import uuid
from collections.abc import Sequence
from datetime import UTC, datetime

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.events import KEY_OFFER_CREATED, enqueue
from app.models import Offer, OfferStatus
from app.schemas import OfferCreate, OfferUpdate


class OfferNotFound(Exception):
    pass


class NotOfferOwner(Exception):
    """A seller may only touch their own offer."""


class OfferNotPending(Exception):
    """Once an offer has been decided, its terms are frozen: an approved offer may
    already have contact permission granted against it, so letting the seller rewrite
    the price afterwards would change what the admin actually approved."""


# Requests in these states are no longer taking offers. OFFER_PENDING still is: several
# sellers compete on the same demand until an admin picks one.
REQUEST_STATES_CLOSED_TO_OFFERS = frozenset({"OFFER_APPROVED", "CLOSED", "CANCELLED"})


class RequestNotAcceptingOffers(Exception):
    def __init__(self, status: str):
        super().__init__(f"request is {status}")
        self.status = status


async def create_offer(
    session: AsyncSession,
    seller_id: uuid.UUID,
    payload: OfferCreate,
    admin_user_ids: Sequence[uuid.UUID] = (),
) -> Offer:
    """R6: a new offer always starts PENDING. The caller cannot choose otherwise -
    status is not part of OfferCreate at all.

    The NEW_OFFER event of flow 2 step 7 is written to the outbox in this same
    transaction, so the offer and the promise to tell the admins about it either both
    exist or neither does. admin_user_ids is empty when auth-service could not be
    reached, in which case there is simply nothing to announce.
    """
    offer = Offer(
        seller_id=seller_id,
        request_id=payload.request_id,
        available_quantity=payload.available_quantity,
        price_per_unit=payload.price_per_unit,
        currency=payload.currency,
        description=payload.description,
        status=OfferStatus.PENDING,
    )
    session.add(offer)

    # Flushed before the event is built so the offer's own columns are settled - the
    # message quotes the terms an admin is being asked to review.
    await session.flush()

    enqueue(
        session,
        KEY_OFFER_CREATED,
        [
            {
                "userId": str(admin_id),
                "type": "NEW_OFFER",
                "title": "An offer is waiting for review",
                "message": (
                    f"A seller offered {offer.available_quantity} at "
                    f"{offer.price_per_unit} {offer.currency} per unit."
                ),
            }
            for admin_id in admin_user_ids
        ],
    )

    await session.commit()
    await session.refresh(offer)
    return offer


async def get_offer(session: AsyncSession, offer_id: uuid.UUID) -> Offer:
    offer = await session.get(Offer, offer_id)
    if offer is None:
        raise OfferNotFound
    return offer


async def list_for_request(session: AsyncSession, request_id: uuid.UUID) -> Sequence[Offer]:
    stmt = select(Offer).where(Offer.request_id == request_id).order_by(Offer.created_at)
    return (await session.scalars(stmt)).all()


async def list_for_seller(session: AsyncSession, seller_id: uuid.UUID) -> Sequence[Offer]:
    stmt = select(Offer).where(Offer.seller_id == seller_id).order_by(Offer.created_at.desc())
    return (await session.scalars(stmt)).all()


async def list_pending(session: AsyncSession, limit: int, offset: int) -> Sequence[Offer]:
    stmt = (
        select(Offer)
        .where(Offer.status == OfferStatus.PENDING)
        .order_by(Offer.created_at)
        .limit(limit)
        .offset(offset)
    )
    return (await session.scalars(stmt)).all()


async def update_offer(
    session: AsyncSession, offer_id: uuid.UUID, seller_id: uuid.UUID, payload: OfferUpdate
) -> Offer:
    offer = await _own_pending_offer(session, offer_id, seller_id)

    offer.available_quantity = payload.available_quantity
    offer.price_per_unit = payload.price_per_unit
    offer.currency = payload.currency
    offer.description = payload.description
    offer.updated_at = _now()

    await session.commit()
    await session.refresh(offer)
    return offer


async def cancel_offer(session: AsyncSession, offer_id: uuid.UUID, seller_id: uuid.UUID) -> Offer:
    """Cancelling is a status change, not a delete: the offer stays as a record of what
    was proposed, which the audit history in Admin/Contact refers back to."""
    offer = await _own_pending_offer(session, offer_id, seller_id)

    offer.status = OfferStatus.CANCELLED
    offer.updated_at = _now()

    await session.commit()
    await session.refresh(offer)
    return offer


async def set_status(session: AsyncSession, offer_id: uuid.UUID, new_status: str) -> Offer:
    """Records an admin decision relayed by Admin/Contact. Only a PENDING offer can be
    decided, so a second approval - or approving something already cancelled - is
    refused rather than silently overwriting the first outcome."""
    offer = await get_offer(session, offer_id)
    if offer.status != OfferStatus.PENDING:
        raise OfferNotPending

    offer.status = new_status
    offer.updated_at = _now()

    await session.commit()
    await session.refresh(offer)
    return offer


async def _own_pending_offer(
    session: AsyncSession, offer_id: uuid.UUID, seller_id: uuid.UUID
) -> Offer:
    offer = await get_offer(session, offer_id)
    if offer.seller_id != seller_id:
        # Deliberately distinct from OfferNotFound: the caller is authenticated and the
        # offer exists, they simply do not own it.
        raise NotOfferOwner
    if offer.status != OfferStatus.PENDING:
        raise OfferNotPending
    return offer


def _now() -> datetime:
    return datetime.now(UTC)
