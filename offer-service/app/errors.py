"""Maps domain and framework errors onto the platform's error shape.

FastAPI's defaults would emit {"detail": ...} and answer a validation failure with 422.
Both are overridden so a client sees one error contract across all five services:
{"message": ..., "status": ...}, with a bad body reported as 400.
"""

import logging

from fastapi import FastAPI, Request, status
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException as StarletteHTTPException

from app.clients import RequestMissing, SellerProfileMissing, UpstreamUnavailable
from app.service import (
    NotOfferOwner,
    OfferAlreadyMade,
    OfferNotFound,
    OfferNotPending,
)

log = logging.getLogger(__name__)


def error_response(status_code: int, message: str) -> JSONResponse:
    return JSONResponse(
        status_code=status_code, content={"message": message, "status": status_code}
    )


def register_exception_handlers(app: FastAPI) -> None:
    @app.exception_handler(StarletteHTTPException)
    async def http_exception(_: Request, exc: StarletteHTTPException) -> JSONResponse:
        detail = exc.detail if isinstance(exc.detail, str) else "Request failed"
        return error_response(exc.status_code, detail)

    @app.exception_handler(RequestValidationError)
    async def validation_error(_: Request, exc: RequestValidationError) -> JSONResponse:
        return error_response(status.HTTP_400_BAD_REQUEST, _first_validation_message(exc))

    @app.exception_handler(OfferNotFound)
    async def offer_not_found(*_: object) -> JSONResponse:
        return error_response(status.HTTP_404_NOT_FOUND, "Offer not found")

    @app.exception_handler(NotOfferOwner)
    async def not_owner(*_: object) -> JSONResponse:
        return error_response(status.HTTP_403_FORBIDDEN, "This offer belongs to another seller")

    @app.exception_handler(OfferNotPending)
    async def not_pending(*_: object) -> JSONResponse:
        return error_response(
            status.HTTP_409_CONFLICT, "Offer is no longer pending and can no longer be changed"
        )

    @app.exception_handler(OfferAlreadyMade)
    async def already_offered(_: Request, exc: OfferAlreadyMade) -> JSONResponse:
        # Carries the offer, the way request-service's duplicate-name refusal carries the
        # request: refusing without saying what to change instead leaves the caller
        # nowhere to go. The full projection, because it is the seller's own offer.
        return JSONResponse(
            status_code=status.HTTP_409_CONFLICT,
            content={
                "message": (
                    "You already have an offer on this request. Answering the same demand "
                    "twice is one offer changed, not two - update that one instead."
                ),
                "status": status.HTTP_409_CONFLICT,
                "existing": exc.existing.model_dump(by_alias=True, mode="json"),
            },
        )

    @app.exception_handler(RequestMissing)
    async def request_missing(*_: object) -> JSONResponse:
        # R5: an offer only makes sense against demand that exists.
        return error_response(status.HTTP_404_NOT_FOUND, "Request not found")

    @app.exception_handler(SellerProfileMissing)
    async def seller_missing(*_: object) -> JSONResponse:
        return error_response(
            status.HTTP_403_FORBIDDEN, "No seller profile exists for this account"
        )

    @app.exception_handler(UpstreamUnavailable)
    async def upstream_down(_: Request, exc: UpstreamUnavailable) -> JSONResponse:
        log.error("upstream unavailable: %s", exc)
        return error_response(status.HTTP_503_SERVICE_UNAVAILABLE, "A dependency is unavailable")

    @app.exception_handler(Exception)
    async def unhandled(request: Request, exc: Exception) -> JSONResponse:
        # Logged with detail, reported without: an internal message must not reach a client.
        log.exception("unhandled error on %s", request.url.path, exc_info=exc)
        return error_response(status.HTTP_500_INTERNAL_SERVER_ERROR, "Internal server error")


def _camel(name: str) -> str:
    head, *rest = name.split("_")
    return head + "".join(part.title() for part in rest)


def _first_validation_message(exc: RequestValidationError) -> str:
    """Turns Pydantic's error into the platform's wording.

    A bad path variable is reported exactly as the Java and Go services report it, so
    the same mistake reads the same way whichever service the client hit. Body errors
    keep Pydantic's detail, which is genuinely useful, but never echo the submitted
    value back.
    """
    errors = exc.errors()
    if not errors:
        return "Invalid request"

    first = errors[0]
    location = list(first.get("loc", ()))
    kind = location[0] if location else "body"
    field = ".".join(_camel(str(part)) for part in location[1:])

    if kind == "path":
        return f"Invalid value for parameter '{field}'" if field else "Invalid path parameter"

    # Pydantic prefixes anything a validator raises with "Value error, ". That names the
    # exception class it caught, which is Pydantic's business and not the caller's.
    message = first.get("msg", "is invalid").removeprefix("Value error, ")
    return f"{field}: {message}" if field else message
