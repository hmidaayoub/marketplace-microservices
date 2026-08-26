"""SQLAlchemy models. Alembic owns the schema outright - nothing here creates or
alters a table, mirroring the ddl-auto: validate arrangement in the Java services."""

import uuid
from datetime import datetime

from sqlalchemy import CheckConstraint, DateTime, Index, String, func
from sqlalchemy.dialects.postgresql import UUID as PgUUID
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    pass


class Channel:
    IN_APP = "IN_APP"
    EMAIL = "EMAIL"
    SMS = "SMS"
    PUSH = "PUSH"

    ALL = (IN_APP, EMAIL, SMS, PUSH)


class NotificationStatus:
    PENDING = "PENDING"
    SENT = "SENT"
    FAILED = "FAILED"
    READ = "READ"

    ALL = (PENDING, SENT, FAILED, READ)

    # What counts towards the unread badge. READ is obviously excluded; FAILED is too,
    # because a notification that never reached the user is not something they can be
    # asked to catch up on - it is an operational problem, not an unread message.
    UNREAD = (PENDING, SENT)


class NotificationType:
    """The events of spec section 18.

    Declared as constants rather than a database enum on purpose: the spec introduces
    this list with "Examples", so a new event must not require a migration in this
    service. The column is shape-checked instead - see notification_type_shape.
    """

    REQUEST_JOINED = "REQUEST_JOINED"
    NEW_OFFER = "NEW_OFFER"
    OFFER_APPROVED = "OFFER_APPROVED"
    OFFER_REJECTED = "OFFER_REJECTED"
    CONTACT_ACCESS_GRANTED = "CONTACT_ACCESS_GRANTED"
    REQUEST_CLOSED = "REQUEST_CLOSED"

    KNOWN = (
        REQUEST_JOINED,
        NEW_OFFER,
        OFFER_APPROVED,
        OFFER_REJECTED,
        CONTACT_ACCESS_GRANTED,
        REQUEST_CLOSED,
    )


class Notification(Base):
    __tablename__ = "notification"

    notification_id: Mapped[uuid.UUID] = mapped_column(
        PgUUID(as_uuid=True), primary_key=True, server_default=func.gen_random_uuid()
    )

    # The recipient, as a global userId. This service stores no customerId or sellerId:
    # the producer has already resolved the identity by the time it calls here, which is
    # what keeps this service free of any dependency on the others.
    user_id: Mapped[uuid.UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False)

    type: Mapped[str] = mapped_column(String(64), nullable=False)
    channel: Mapped[str] = mapped_column(String(16), nullable=False, server_default=Channel.IN_APP)

    title: Mapped[str] = mapped_column(String(200), nullable=False)
    message: Mapped[str] = mapped_column(String, nullable=False)

    status: Mapped[str] = mapped_column(
        String(16), nullable=False, server_default=NotificationStatus.PENDING
    )

    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )
    sent_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)

    __table_args__ = (
        CheckConstraint(
            "status IN ('PENDING', 'SENT', 'FAILED', 'READ')",
            name="notification_status_valid",
        ),
        CheckConstraint(
            "channel IN ('IN_APP', 'EMAIL', 'SMS', 'PUSH')",
            name="notification_channel_valid",
        ),
        # Open set, fixed shape: any SCREAMING_SNAKE event is accepted, so adding an
        # event to the platform never means migrating this table.
        CheckConstraint("type ~ '^[A-Z][A-Z0-9_]*$'", name="notification_type_shape"),
        CheckConstraint("length(btrim(title)) > 0", name="notification_title_not_blank"),
        # A notification is SENT or READ only once it has actually gone out, and it can
        # never be PENDING with a send time. Keeps sent_at from drifting from status.
        CheckConstraint(
            "(status IN ('SENT', 'READ')) = (sent_at IS NOT NULL)",
            name="notification_sent_at_matches_status",
        ),
        # Serves GET /api/notifications/me: one user's inbox, newest first.
        Index("idx_notification_user_created", "user_id", "created_at"),
        # Serves the unread count without scanning the user's whole history.
        Index("idx_notification_user_status", "user_id", "status"),
    )
