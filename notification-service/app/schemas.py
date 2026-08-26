"""Wire schemas. Kept separate from the SQLAlchemy models so a response can withhold a
column, the same projection discipline the other services apply to addresses, phone
numbers and customerIds."""

import uuid
from datetime import datetime
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator

from app.models import Channel, Notification

# Open set, fixed shape - see NotificationType. The pattern is the same one the column's
# CHECK constraint enforces, so a payload the API accepts can never fail at the database.
TYPE_PATTERN = r"^[A-Z][A-Z0-9_]*$"

# A fan-out is bounded so one call cannot pin the event loop or the transaction. The
# largest real case is REQUEST_CLOSED to every participant of a request.
MAX_BULK = 500


class NotificationCreate(BaseModel):
    """The body of an internal create. Only a service holding the shared key can reach
    it, and even then it may not choose the lifecycle: status and notificationId are not
    fields here at all, and extra="forbid" means smuggling one in is a 400 rather than a
    value that gets silently dropped."""

    model_config = ConfigDict(extra="forbid")

    user_id: uuid.UUID = Field(alias="userId")
    type: str = Field(pattern=TYPE_PATTERN, max_length=64)
    channel: Literal["IN_APP", "EMAIL", "SMS", "PUSH"] = Channel.IN_APP
    title: str = Field(min_length=1, max_length=200)
    message: str = Field(min_length=1, max_length=2000)

    @field_validator("title", "message")
    @classmethod
    def not_blank(cls, value: str) -> str:
        stripped = value.strip()
        if not stripped:
            raise ValueError("must not be blank")
        return stripped


class NotificationBulkCreate(BaseModel):
    """A fan-out. Each entry is a full notification rather than one message broadcast to
    a list of ids, so a producer can vary the wording per recipient - and the common
    case of an identical message to many users is just the same object repeated."""

    model_config = ConfigDict(extra="forbid")

    notifications: list[NotificationCreate] = Field(min_length=1, max_length=MAX_BULK)


class EventEnvelope(BaseModel):
    """One AMQP message (see docs/events.md).

    extra="forbid" here as well as on the payloads: a producer that adds a field the
    consumer does not understand should be a loud dead-letter, not a value silently
    dropped on the floor.
    """

    model_config = ConfigDict(extra="forbid")

    event_id: uuid.UUID = Field(alias="eventId")
    occurred_at: datetime | None = Field(default=None, alias="occurredAt")
    source: str = Field(min_length=1, max_length=64)
    notifications: list[NotificationCreate] = Field(min_length=1, max_length=MAX_BULK)


class NotificationOut(BaseModel):
    notification_id: uuid.UUID = Field(serialization_alias="notificationId")
    user_id: uuid.UUID = Field(serialization_alias="userId")
    type: str
    channel: str
    title: str
    message: str
    status: str
    created_at: datetime = Field(serialization_alias="createdAt")
    sent_at: datetime | None = Field(serialization_alias="sentAt")

    @classmethod
    def of(cls, notification: Notification) -> "NotificationOut":
        return cls(
            notification_id=notification.notification_id,
            user_id=notification.user_id,
            type=notification.type,
            channel=notification.channel,
            title=notification.title,
            message=notification.message,
            status=notification.status,
            created_at=notification.created_at,
            sent_at=notification.sent_at,
        )


class UnreadCountOut(BaseModel):
    unread_count: int = Field(serialization_alias="unreadCount")


class BulkResultOut(BaseModel):
    created: int
    notifications: list[NotificationOut]


__all__ = [
    "MAX_BULK",
    "BulkResultOut",
    "EventEnvelope",
    "NotificationBulkCreate",
    "NotificationCreate",
    "NotificationOut",
    "UnreadCountOut",
]
