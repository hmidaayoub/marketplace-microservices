"""Public API: /api/notifications/** (spec section 13).

Every route here is scoped to the caller. The recipient is taken from the token's sub
claim and used as the query filter, so there is no endpoint that reads someone else's
inbox and no parameter a caller could point at another user.
"""

import uuid
from typing import Annotated, Any

from fastapi import APIRouter, Depends, Query
from sqlalchemy.ext.asyncio import AsyncSession

from app import service
from app.db import get_session
from app.schemas import NotificationOut, UnreadCountOut
from app.security import CurrentPrincipal

router = APIRouter(prefix="/api/notifications", tags=["notifications"])

SessionDep = Annotated[AsyncSession, Depends(get_session)]


@router.get("/me", response_model=None, responses={200: {"model": list[NotificationOut]}})
async def my_notifications(
    principal: CurrentPrincipal,
    session: SessionDep,
    limit: Annotated[int, Query(ge=1, le=100)] = 20,
    offset: Annotated[int, Query(ge=0)] = 0,
    unread_only: Annotated[bool, Query(alias="unreadOnly")] = False,
) -> list[dict[str, Any]]:
    """The caller's own inbox, newest first."""
    notifications = await service.list_for_user(
        session, principal.user_id, limit=limit, offset=offset, unread_only=unread_only
    )
    return [
        NotificationOut.of(notification).model_dump(by_alias=True, mode="json")
        for notification in notifications
    ]


@router.get(
    "/me/unread-count", response_model=None, responses={200: {"model": UnreadCountOut}}
)
async def my_unread_count(principal: CurrentPrincipal, session: SessionDep) -> dict[str, Any]:
    """Backs the unread badge. Counts PENDING and SENT but not FAILED: a notification
    that never reached the user is an operational problem, not an unread message."""
    count = await service.unread_count(session, principal.user_id)
    return UnreadCountOut(unread_count=count).model_dump(by_alias=True, mode="json")


@router.patch(
    "/{notification_id}/read", response_model=None, responses={200: {"model": NotificationOut}}
)
async def mark_read(
    notification_id: uuid.UUID, principal: CurrentPrincipal, session: SessionDep
) -> dict[str, Any]:
    """Marks one of the caller's own notifications read.

    Another user's notification answers 404 rather than 403, so this cannot be used to
    discover which notification ids exist. Repeating the call is fine - see
    service.mark_read for why reading is idempotent where deciding is not.
    """
    notification = await service.mark_read(session, notification_id, principal.user_id)
    return NotificationOut.of(notification).model_dump(by_alias=True, mode="json")
