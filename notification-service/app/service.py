"""Notification domain logic (spec section 13).

The service records notifications and marks them delivered. It never resolves an
identity, reads another service's data, or decides who should be told about an event -
the producer has done all of that by the time it calls here. That is what "not the
source of truth for business data" means in practice, and it is why this is the only
service in the platform with no outbound dependencies.
"""

import logging
import uuid
from collections.abc import Sequence
from datetime import UTC, datetime

from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import Channel, Notification, NotificationStatus
from app.schemas import NotificationCreate

log = logging.getLogger(__name__)


class NotificationNotFound(Exception):
    """Raised both when no such notification exists and when it belongs to someone
    else.

    Deliberately one exception for two cases. Answering 403 for another user's
    notification would confirm that the id exists, which turns this endpoint into an
    oracle for enumerating other people's notifications. The caller gets 404 either way
    and learns nothing about ids that are not theirs.
    """


def _now() -> datetime:
    return datetime.now(UTC)


def _deliver(notification: Notification) -> None:
    """Applies the delivery outcome for a new notification.

    IN_APP needs no provider: the row *is* the delivery, so storing it is sending it and
    the notification goes straight to SENT. EMAIL, SMS and PUSH have no provider wired
    in this MVP, so they are left PENDING for a dispatcher to pick up rather than being
    marked SENT on the strength of nothing having happened. The status column therefore
    always reflects a real outcome.
    """
    if notification.channel == Channel.IN_APP:
        notification.status = NotificationStatus.SENT
        notification.sent_at = _now()
    else:
        notification.status = NotificationStatus.PENDING
        notification.sent_at = None


def _build(payload: NotificationCreate) -> Notification:
    notification = Notification(
        user_id=payload.user_id,
        type=payload.type,
        channel=payload.channel,
        title=payload.title,
        message=payload.message,
    )
    _deliver(notification)
    return notification


async def create(session: AsyncSession, payload: NotificationCreate) -> Notification:
    notification = _build(payload)
    session.add(notification)
    await session.commit()
    await session.refresh(notification)
    return notification


async def create_bulk(
    session: AsyncSession, payloads: Sequence[NotificationCreate]
) -> Sequence[Notification]:
    """One transaction for the whole fan-out.

    A partially delivered REQUEST_CLOSED - some participants told, others not, with no
    record of which - is worse than none delivered at all, because the producer cannot
    safely retry it. All or nothing means a failed call can simply be repeated.
    """
    notifications = [_build(payload) for payload in payloads]
    session.add_all(notifications)
    await session.commit()
    for notification in notifications:
        await session.refresh(notification)
    return notifications


async def list_for_user(
    session: AsyncSession,
    user_id: uuid.UUID,
    *,
    limit: int,
    offset: int,
    unread_only: bool = False,
) -> Sequence[Notification]:
    stmt = select(Notification).where(Notification.user_id == user_id)
    if unread_only:
        stmt = stmt.where(Notification.status.in_(NotificationStatus.UNREAD))
    stmt = stmt.order_by(Notification.created_at.desc()).limit(limit).offset(offset)
    return (await session.scalars(stmt)).all()


async def unread_count(session: AsyncSession, user_id: uuid.UUID) -> int:
    stmt = (
        select(func.count())
        .select_from(Notification)
        .where(
            Notification.user_id == user_id,
            Notification.status.in_(NotificationStatus.UNREAD),
        )
    )
    return (await session.scalar(stmt)) or 0


async def mark_read(
    session: AsyncSession, notification_id: uuid.UUID, user_id: uuid.UUID
) -> Notification:
    """Marks one of the caller's own notifications read.

    Idempotent by design, unlike the decide-and-revoke calls elsewhere in the platform
    that answer 409 on a repeat. Those are decisions, where a second attempt means the
    caller believes something that is no longer true. Reading is not a decision - a user
    clicking twice, or two devices syncing the same inbox, is ordinary - so a repeat
    returns the notification unchanged instead of an error.

    A notification that was never delivered cannot be read, so FAILED and PENDING are
    left alone rather than being quietly marked READ.
    """
    notification = await session.get(Notification, notification_id)

    # The ownership check is part of the lookup, not a separate authorisation step, so
    # there is no path that returns another user's notification.
    if notification is None or notification.user_id != user_id:
        raise NotificationNotFound

    if notification.status == NotificationStatus.SENT:
        notification.status = NotificationStatus.READ
        await session.commit()
        await session.refresh(notification)
    elif notification.status != NotificationStatus.READ:
        log.info(
            "not marking notification %s read: status is %s",
            notification_id,
            notification.status,
        )

    return notification
