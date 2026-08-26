"""Maps domain and framework errors onto the platform's error shape.

FastAPI's defaults would emit {"detail": ...} and answer a validation failure with 422.
Both are overridden so a client sees one error contract across all six services:
{"message": ..., "status": ...}, with a bad body reported as 400.
"""

import logging

from fastapi import FastAPI, Request, status
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from starlette.exceptions import HTTPException as StarletteHTTPException

from app.service import NotificationNotFound

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

    @app.exception_handler(NotificationNotFound)
    async def notification_not_found(*_: object) -> JSONResponse:
        # Also the answer for another user's notification - see NotificationNotFound.
        return error_response(status.HTTP_404_NOT_FOUND, "Notification not found")

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

    message = first.get("msg", "is invalid")
    return f"{field}: {message}" if field else message
