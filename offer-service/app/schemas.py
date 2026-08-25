"""Wire schemas. Kept separate from the SQLAlchemy models so a response can withhold a
column - the same projection discipline the other services apply to addresses, phone
numbers and customerIds."""

import uuid
from datetime import datetime
from decimal import Decimal
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator

from app.models import Offer, OfferStatus

CURRENCY_PATTERN = r"^[A-Z]{3}$"


class OfferCreate(BaseModel):
    # Forbid unknown fields so a caller cannot smuggle in sellerId or status and have it
    # silently ignored - identity and lifecycle are owned by the service, not the client.
    model_config = ConfigDict(extra="forbid")

    request_id: uuid.UUID = Field(alias="requestId")
    available_quantity: int = Field(alias="availableQuantity", gt=0)
    price_per_unit: Decimal = Field(alias="pricePerUnit", gt=0, max_digits=12, decimal_places=2)
    currency: str = Field(pattern=CURRENCY_PATTERN)
    description: str = Field(default="", max_length=2000)

    @field_validator("currency", mode="before")
    @classmethod
    def upper(cls, value: str) -> str:
        return value.upper() if isinstance(value, str) else value


class OfferUpdate(BaseModel):
    model_config = ConfigDict(extra="forbid")

    available_quantity: int = Field(alias="availableQuantity", gt=0)
    price_per_unit: Decimal = Field(alias="pricePerUnit", gt=0, max_digits=12, decimal_places=2)
    currency: str = Field(pattern=CURRENCY_PATTERN)
    description: str = Field(default="", max_length=2000)

    @field_validator("currency", mode="before")
    @classmethod
    def upper(cls, value: str) -> str:
        return value.upper() if isinstance(value, str) else value


class StatusUpdate(BaseModel):
    """Body of the internal decision call. Admin/Contact owns the decision itself
    (R7); this service only records the resulting status."""

    model_config = ConfigDict(extra="forbid")

    status: Literal["APPROVED", "REJECTED"]


class OfferOut(BaseModel):
    offer_id: uuid.UUID = Field(serialization_alias="offerId")
    request_id: uuid.UUID = Field(serialization_alias="requestId")
    seller_id: uuid.UUID = Field(serialization_alias="sellerId")
    available_quantity: int = Field(serialization_alias="availableQuantity")
    price_per_unit: Decimal = Field(serialization_alias="pricePerUnit")
    currency: str
    description: str
    status: str
    created_at: datetime = Field(serialization_alias="createdAt")
    updated_at: datetime = Field(serialization_alias="updatedAt")

    @classmethod
    def of(cls, offer: Offer) -> "OfferOut":
        return cls(
            offer_id=offer.offer_id,
            request_id=offer.request_id,
            seller_id=offer.seller_id,
            available_quantity=offer.available_quantity,
            price_per_unit=offer.price_per_unit,
            currency=offer.currency,
            description=offer.description,
            status=offer.status,
            created_at=offer.created_at,
            updated_at=offer.updated_at,
        )


class CompetingOfferOut(BaseModel):
    """What one seller is allowed to learn about another seller's offer on the same
    request: that it exists and what it costs, never who made it.

    Sellers still need to see the competitive field to price sensibly, but sellerId is
    withheld so a marketplace participant cannot build a picture of who they are
    bidding against. Customers and admins get the full OfferOut instead.
    """

    offer_id: uuid.UUID = Field(serialization_alias="offerId")
    request_id: uuid.UUID = Field(serialization_alias="requestId")
    available_quantity: int = Field(serialization_alias="availableQuantity")
    price_per_unit: Decimal = Field(serialization_alias="pricePerUnit")
    currency: str
    status: str
    created_at: datetime = Field(serialization_alias="createdAt")

    @classmethod
    def of(cls, offer: Offer) -> "CompetingOfferOut":
        return cls(
            offer_id=offer.offer_id,
            request_id=offer.request_id,
            available_quantity=offer.available_quantity,
            price_per_unit=offer.price_per_unit,
            currency=offer.currency,
            status=offer.status,
            created_at=offer.created_at,
        )


__all__ = [
    "CompetingOfferOut",
    "OfferCreate",
    "OfferOut",
    "OfferStatus",
    "OfferUpdate",
    "StatusUpdate",
]
