"""Wire schemas. Kept separate from the SQLAlchemy models so a response can withhold a
column - the same projection discipline the other services apply to addresses, phone
numbers and customerIds."""

import uuid
from datetime import datetime
from decimal import Decimal
from typing import Literal

from pydantic import BaseModel, ConfigDict, Field, field_validator, model_validator

from app.models import Offer, OfferStatus

CURRENCY_PATTERN = r"^[A-Z]{3}$"


class RequestedItem(BaseModel):
    """The item a seller is offering when no request carries it yet.

    Demand and supply do not have to arrive in that order. A seller holding stock nobody
    has asked for still has something to say, and naming the item here is what lets
    request-service open the request their offer hangs on - with no buyers on it, waiting
    for the first one to join. The fields are request-service's own, because that is who
    will hold them.
    """

    model_config = ConfigDict(extra="forbid")

    item_name: str = Field(alias="itemName", max_length=200)
    description: str = Field(default="", max_length=2000)
    category: str = Field(default="", max_length=100)

    @field_validator("item_name", "description", "category")
    @classmethod
    def trimmed(cls, value: str) -> str:
        return value.strip()

    @field_validator("item_name")
    @classmethod
    def not_blank(cls, value: str) -> str:
        if not value:
            raise ValueError("must not be blank")
        return value


class OfferCreate(BaseModel):
    """R5: an offer names the demand it answers - either a request that already exists,
    or the item it is for.

    Exactly one of the two. requestId is the ordinary case, a seller bidding on demand
    they browsed to. item is the other direction: nothing carries this product yet, so
    the request is opened as part of storing the offer and the buyers arrive afterwards.
    Sending both would be two answers to one question, and the service would have to pick
    one - so it asks rather than guessing.
    """

    # Forbid unknown fields so a caller cannot smuggle in sellerId or status and have it
    # silently ignored - identity and lifecycle are owned by the service, not the client.
    model_config = ConfigDict(extra="forbid")

    request_id: uuid.UUID | None = Field(default=None, alias="requestId")
    item: RequestedItem | None = None
    available_quantity: int = Field(alias="availableQuantity", gt=0)
    price_per_unit: Decimal = Field(alias="pricePerUnit", gt=0, max_digits=12, decimal_places=2)
    currency: str = Field(pattern=CURRENCY_PATTERN)
    description: str = Field(default="", max_length=2000)

    @field_validator("currency", mode="before")
    @classmethod
    def upper(cls, value: str) -> str:
        return value.upper() if isinstance(value, str) else value

    @model_validator(mode="after")
    def one_way_of_naming_the_demand(self) -> "OfferCreate":
        if (self.request_id is None) == (self.item is None):
            raise ValueError(
                "Name the demand this offer answers: either requestId, for a request "
                "that already exists, or item, for a product no request carries yet - "
                "one of the two, not both"
            )
        return self


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

    # Whether /api/offers/{id}/image will answer with a picture. A flag and not the
    # bytes: an admin queue of twenty offers would otherwise be twenty megabytes to
    # render a list that shows none of them.
    has_image: bool = Field(default=False, serialization_alias="hasImage")

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
            # The media type is stored on the offer precisely so this needs no join: it
            # is empty for every offer that carries no picture.
            has_image=bool(offer.image_type),
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
    "RequestedItem",
    "StatusUpdate",
]
