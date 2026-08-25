"""SQLAlchemy models. Alembic owns the schema outright - nothing here creates or
alters a table, mirroring the ddl-auto: validate arrangement in the Java services."""

import uuid
from datetime import datetime
from decimal import Decimal

from sqlalchemy import CheckConstraint, DateTime, Index, Integer, Numeric, String, func
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
