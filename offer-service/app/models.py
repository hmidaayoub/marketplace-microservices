"""SQLAlchemy models. Alembic owns the schema outright - nothing here creates or
alters a table, mirroring the ddl-auto: validate arrangement in the Java services."""

import uuid
from datetime import datetime
from decimal import Decimal

from sqlalchemy import CheckConstraint, DateTime, Index, Integer, Numeric, String, func, text
from sqlalchemy.dialects.postgresql import JSONB
from sqlalchemy.dialects.postgresql import UUID as PgUUID
from sqlalchemy.orm import DeclarativeBase, Mapped, mapped_column


class Base(DeclarativeBase):
    pass


class OfferStatus:
    PENDING = "PENDING"
    APPROVED = "APPROVED"
    REJECTED = "REJECTED"
    CANCELLED = "CANCELLED"

    ALL = (PENDING, APPROVED, REJECTED, CANCELLED)
    #  R7: only an admin decides, and only on an offer still awaiting review.
    DECIDABLE = (APPROVED, REJECTED)


class NotificationOutbox(Base):
    """Transactional outbox for notification events (see docs/events.md).

    Publishing used to happen after the offer was stored, which meant a broker that was
    down cost the notification outright: the offer existed and no admin was ever told.
    The event is now written here in the same transaction as the offer, so the two
    either both happen or neither does, and a relay moves rows to the broker afterwards.

    That converts "lost while the broker is down" into "delivered late", which is the
    only honest guarantee available without distributed transactions.
    """

    __tablename__ = "notification_outbox"

    outbox_id: Mapped[uuid.UUID] = mapped_column(
        PgUUID(as_uuid=True), primary_key=True, server_default=func.gen_random_uuid()
    )

    # Becomes the envelope's eventId. Generated here rather than at publish time so a
    # relay that publishes a row twice - it died between the publish and the commit that
    # marks it sent - produces the same id both times, and the consumer's processed_event
    # table recognises the redelivery instead of duplicating the notification.
    event_id: Mapped[uuid.UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False, unique=True)

    routing_key: Mapped[str] = mapped_column(String, nullable=False)
    payload: Mapped[list] = mapped_column(JSONB, nullable=False)

    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )
    published_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True), nullable=True)
    attempts: Mapped[int] = mapped_column(Integer, nullable=False, server_default="0")
    last_error: Mapped[str | None] = mapped_column(String, nullable=True)

    __table_args__ = (
        CheckConstraint(
            "length(btrim(routing_key)) > 0", name="notification_outbox_routing_key_not_blank"
        ),
        # The relay's only query: the oldest unpublished rows. Partial, so it stays small
        # no matter how much history the table accumulates.
        Index(
            "idx_notification_outbox_pending",
            "created_at",
            postgresql_where=text("published_at IS NULL"),
        ),
    )


class Offer(Base):
    __tablename__ = "offer"

    offer_id: Mapped[uuid.UUID] = mapped_column(
        PgUUID(as_uuid=True), primary_key=True, server_default=func.gen_random_uuid()
    )
    seller_id: Mapped[uuid.UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False)
    request_id: Mapped[uuid.UUID] = mapped_column(PgUUID(as_uuid=True), nullable=False)

    available_quantity: Mapped[int] = mapped_column(Integer, nullable=False)
    price_per_unit: Mapped[Decimal] = mapped_column(Numeric(12, 2), nullable=False)
    currency: Mapped[str] = mapped_column(String(3), nullable=False)
    description: Mapped[str] = mapped_column(String, nullable=False, server_default="")

    status: Mapped[str] = mapped_column(
        String(16), nullable=False, server_default=OfferStatus.PENDING
    )

    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True), nullable=False, server_default=func.now()
    )

    __table_args__ = (
        CheckConstraint(
            "status IN ('PENDING', 'APPROVED', 'REJECTED', 'CANCELLED')",
            name="offer_status_valid",
        ),
        CheckConstraint("available_quantity > 0", name="offer_quantity_positive"),
        CheckConstraint("price_per_unit > 0", name="offer_price_positive"),
        CheckConstraint("currency ~ '^[A-Z]{3}$'", name="offer_currency_iso4217_shape"),
        Index("idx_offer_request", "request_id"),
        Index("idx_offer_seller", "seller_id"),
        # Serves GET /internal/offers/pending, the admin review queue.
        Index("idx_offer_status_created", "status", "created_at"),
    )
