"""Internal API: /internal/notifications/** - service-to-service only, never routed by
the public gateway (spec section 6).

This is how the events of spec section 18 arrive: REQUEST_JOINED and REQUEST_CLOSED
from request-service, NEW_OFFER from offer-service, and OFFER_APPROVED,
OFFER_REJECTED and CONTACT_ACCESS_GRANTED from admin-service. The producer has already
decided who the recipient is; this service records and delivers.
"""

from typing import Annotated, Any

from fastapi import APIRouter, Depends, status
from sqlalchemy.ext.asyncio import AsyncSession

from app import service
from app.db import get_session
from app.schemas import BulkResultOut, NotificationBulkCreate, NotificationCreate, NotificationOut
from app.security import require_internal_api_key

router = APIRouter(
    prefix="/internal/notifications",
    tags=["internal"],
    dependencies=[Depends(require_internal_api_key)],
)

SessionDep = Annotated[AsyncSession, Depends(get_session)]


@router.post("", status_code=status.HTTP_201_CREATED, response_model=None)
async def create_notification(payload: NotificationCreate, session: SessionDep) -> dict[str, Any]:
    """Creates and delivers one notification."""
    notification = await service.create(session, payload)
    return NotificationOut.of(notification).model_dump(by_alias=True, mode="json")


@router.post("/bulk", status_code=status.HTTP_201_CREATED, response_model=None)
async def create_notifications(
    payload: NotificationBulkCreate, session: SessionDep
) -> dict[str, Any]:
    """Creates and delivers a fan-out in one transaction.

    This is what REQUEST_CLOSED needs: every participant of a request told, or none of
    them, so a producer whose call failed can simply retry it.
    """
    notifications = await service.create_bulk(session, payload.notifications)
    return BulkResultOut(
        created=len(notifications),
        notifications=[NotificationOut.of(n) for n in notifications],
    ).model_dump(by_alias=True, mode="json")
