"""Offer domain logic (spec section 11)."""

import uuid
from collections.abc import Sequence
from datetime import UTC, datetime

from sqlalchemy import func, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.ext.asyncio import AsyncSession

from app.events import KEY_OFFER_CREATED, enqueue
from app.models import Offer, OfferStatus
from app.schemas import OfferCreate, OfferOut, OfferUpdate


class OfferNotFound(Exception):
    pass


class NotOfferOwner(Exception):
    """A seller may only touch their own offer."""


class OfferAlreadyMade(Exception):
    """The seller already has a live offer on this request.

    Answering the same demand twice is one proposal changed, not two - so it carries the
    offer that already exists, because "you cannot do this" without saying what to do
    instead leaves the seller nowhere to go. Updating it is PUT /api/offers/{offerId},
    which is the call this refusal points at.

    It carries the projection rather than the ORM instance on purpose. get_session rolls
    the session back when a handler raises, which expires every instance loaded from it,
    and the exception handler that renders this runs after the session has closed - so a
    live object would be detached and unreadable by the time anyone asked it anything.
    """

    def __init__(self, existing: Offer):
        super().__init__("this seller already has a live offer on this request")
        self.existing = OfferOut.of(existing)


class OfferNotPending(Exception):
    """Once an offer has been decided, its terms are frozen: an approved offer may
    already have contact permission granted against it, so letting the seller rewrite
    the price afterwards would change what the admin actually approved."""


# Nothing here refuses an offer on the strength of the request's status. A request is
# OPEN or INACTIVE and neither ends it, so there is no state in which demand has gone
# for good and an offer against it is a mistake.
#
# What does refuse one is the seller having answered this request already - see
# OfferAlreadyMade. That is a rule about the seller, not about the demand: other sellers
# still compete freely on the same request, and an approval does not close the door on
# the ones who come after.


async def create_offer(
    session: AsyncSession,
    seller_id: uuid.UUID,
    request_id: uuid.UUID,
    payload: OfferCreate,
    admin_user_ids: Sequence[uuid.UUID] = (),
) -> Offer:
    """R6: a new offer always starts PENDING. The caller cannot choose otherwise -
    status is not part of OfferCreate at all.

    request_id is passed rather than read off the payload, because the payload may not
    carry one: a seller who named an item instead had the request resolved - or opened -
    by request-service on the way in. Either way an offer is stored against a request
    that exists, which is all this layer needs to be true.

    A seller answers a request once: a second offer against demand they have already bid
    on is refused with the first one attached, to be updated instead. The read below is
    what makes that a clear 409 rather than a constraint violation, and the unique index
    behind it is what makes it true under a race the read cannot see.

    The NEW_OFFER event of flow 2 step 7 is written to the outbox in this same
    transaction, so the offer and the promise to tell the admins about it either both
    exist or neither does. admin_user_ids is empty when auth-service could not be
    reached, in which case there is simply nothing to announce.
    """
    existing = await live_offer_of(session, seller_id, request_id)
    if existing is not None:
        raise OfferAlreadyMade(existing)

    offer = Offer(
        seller_id=seller_id,
        request_id=request_id,
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

    try:
        await session.commit()
    except IntegrityError as exc:
        # The index, not the read above, is what makes a double offer impossible: two
        # submissions arriving together would both find nothing and both insert.
        await session.rollback()
        existing = await live_offer_of(session, seller_id, request_id)
        if existing is None:
            raise
        raise OfferAlreadyMade(existing) from exc

    await session.refresh(offer)
    return offer


async def live_offer_of(
    session: AsyncSession, seller_id: uuid.UUID, request_id: uuid.UUID
) -> Offer | None:
    """The seller's standing offer on this request, if they have one.

    Live is PENDING or APPROVED - a proposal that still stands. A cancelled or rejected
    one is a record of a proposal that does not, and neither can be updated, so treating
    either as "you already offered" would leave the seller unable to offer and unable to
    update.
    """
    stmt = select(Offer).where(
        Offer.seller_id == seller_id,
        Offer.request_id == request_id,
        Offer.status.in_(OfferStatus.LIVE),
    )
    return (await session.scalars(stmt)).first()


async def count_live_offers(session: AsyncSession, request_id: uuid.UUID) -> int:
    """How many offers on this request still stand.

    PENDING or APPROVED - the same set that stops a seller offering twice. A cancelled or
    rejected offer is a record of a proposal rather than one, so it neither blocks a new
    offer nor keeps a request from going dormant.
    """
    stmt = (
        select(func.count())
        .select_from(Offer)
        .where(Offer.request_id == request_id, Offer.status.in_(OfferStatus.LIVE))
    )
    return int(await session.scalar(stmt) or 0)


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
