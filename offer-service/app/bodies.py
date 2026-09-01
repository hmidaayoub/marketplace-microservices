"""Reading a request body that may carry a file alongside its JSON.

Two content types, one handler. A plain application/json body is validated exactly as
FastAPI would have validated it - which is what keeps every existing caller working
untouched: the smoke script, the Postman collection and the Swagger UI all still send
the JSON they always sent, and none of them had to learn about images.

A multipart/form-data body carries the same JSON in a part named "payload", plus an
optional file part named "image". The JSON stays in one part rather than being spread
across form fields because an offer's body is not flat - it names the item it is for as
a nested object - and flattening a structure into form keys would mean a second,
hand-written parser that could disagree with the first about what is valid.

The Go twin of this is request-service's httpx.DecodeJSONOrMultipart. Same contract, so
the frontend posts a request and an offer the same way.
"""

import json

from fastapi import Request, status
from fastapi.exceptions import RequestValidationError
from pydantic import BaseModel, ValidationError

from app import media

# The part names, matching request-service's.
PAYLOAD_PART = "payload"
IMAGE_PART = "image"


class BadBody(Exception):
    """A body that could not be read at all, as opposed to one that failed validation."""

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message
        self.status_code = status.HTTP_400_BAD_REQUEST


async def json_or_multipart[Model: BaseModel](
    request: Request, model: type[Model]
) -> tuple[Model, bytes | None]:
    """Validate the body into `model`, and return any image sent beside it.

    The image comes back as raw bytes, or None when no file part was sent. Its format is
    not decided here: media.detect reads the bytes themselves, because the part's
    declared content type is a claim by the uploader.
    """
    content_type = request.headers.get("content-type", "")

    if not content_type.startswith("multipart/form-data"):
        raw = await request.body()
        if not raw:
            raise BadBody("Request body is required")
        try:
            parsed = json.loads(raw)
        except ValueError as exc:
            raise BadBody("Malformed JSON body") from exc
        return _validate(model, parsed), None

    form = await request.form()

    payload = form.get(PAYLOAD_PART)
    if not isinstance(payload, str) or not payload:
        raise BadBody(f'Multipart body needs a "{PAYLOAD_PART}" part carrying the JSON fields')
    try:
        parsed = json.loads(payload)
    except ValueError as exc:
        raise BadBody("Invalid JSON in the payload part") from exc

    validated = _validate(model, parsed)

    upload = form.get(IMAGE_PART)
    if upload is None or isinstance(upload, str):
        # A form submitted with no picture chosen. The rest of the body still stands.
        return validated, None

    # One byte past the cap, so a file that is exactly too big is still seen to be too
    # big rather than silently truncated to the limit and stored as valid.
    image = await upload.read(media.MAX_BYTES + 1)
    if not image:
        return validated, None
    return validated, image


def _validate[Model: BaseModel](model: type[Model], parsed: object) -> Model:
    """Validation failures are raised as FastAPI's own, so they reach the handler that
    already turns them into the platform's {"message", "status"} 400 - one error shape,
    whichever content type the body arrived as."""
    try:
        return model.model_validate(parsed)
    except ValidationError as exc:
        raise RequestValidationError(exc.errors()) from exc
