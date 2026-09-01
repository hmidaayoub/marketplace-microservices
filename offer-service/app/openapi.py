"""The OpenAPI document this service publishes.

Bearer auth is declared here rather than by swapping the token dependency for FastAPI's
HTTPBearer: the dependency reads the header itself and raises the platform's own 401,
and changing it to get a nicer document would change behaviour the tests pin down. This
is a documentation concern, so it is solved in the document.
"""

from typing import Any

from fastapi import FastAPI
from fastapi.openapi.utils import get_openapi

from app.schemas import OfferCreate, OfferUpdate

BEARER_SCHEME = "bearerAuth"


def customise_openapi(app: FastAPI) -> None:
    """Adds the bearer scheme and drops the Authorization header parameter."""

    def openapi() -> dict[str, Any]:
        if app.openapi_schema:
            return app.openapi_schema

        schema = get_openapi(
            title=app.title,
            version=app.version,
            description=app.description,
            routes=app.routes,
            servers=app.servers,
        )

        # Declared once and applied to every operation. Swagger UI turns this into the
        # Authorize button, so a token is pasted once per session rather than per call.
        schema.setdefault("components", {}).setdefault("securitySchemes", {})[BEARER_SCHEME] = {
            "type": "http",
            "scheme": "bearer",
            "bearerFormat": "JWT",
            "description": "Paste the accessToken from POST /api/auth/login.",
        }
        schema["security"] = [{BEARER_SCHEME: []}]

        # The request bodies FastAPI can no longer infer.
        #
        # submit_offer and update_offer read the body themselves, because each accepts
        # either JSON or a multipart body carrying a picture, and a signature documents
        # one shape per parameter. Their openapi_extra points at these schemas, so the
        # models have to be put into components by hand - nothing else references them
        # any more, and a $ref to a schema that was never emitted is a broken document.
        components = schema.setdefault("components", {}).setdefault("schemas", {})
        for model in (OfferCreate, OfferUpdate):
            generated = model.model_json_schema(ref_template="#/components/schemas/{model}")
            # Nested models - an offer names the item it is for - come back under $defs
            # with the refs already pointing at components, so they only have to be
            # lifted one level.
            for name, nested in generated.pop("$defs", {}).items():
                components.setdefault(name, _without_null_defaults(nested))
            components[model.__name__] = _without_null_defaults(generated)

        # The token arrives through a plain Header parameter, so without this it would
        # also be documented as a free-text `authorization` field on every operation -
        # two places to put the same token, one of which Authorize does not fill in.
        for path_item in schema.get("paths", {}).values():
            for operation in path_item.values():
                parameters = operation.get("parameters")
                if not parameters:
                    continue
                kept = [p for p in parameters if p.get("name", "").lower() != "authorization"]
                if kept:
                    operation["parameters"] = kept
                else:
                    del operation["parameters"]

        app.openapi_schema = schema
        return schema

    app.openapi = openapi


def _without_null_defaults(schema: dict[str, Any]) -> dict[str, Any]:
    """Drop `"default": null` from a schema's properties.

    Pydantic emits it for every optional field; FastAPI's own body generation did not,
    and the difference is not cosmetic. Code generators read a property that carries a
    default as one the caller can count on being there - openapi-typescript turns it
    from `requestId?: string | null` into `requestId: string | null` - so leaving it in
    would make two optional fields look mandatory to every typed client, for a default
    that says nothing: `requestId` absent and `requestId: null` mean the same thing
    here, and the model validator enforces exactly-one-of either way.

    Only null defaults go. A real default is information a caller wants.
    """
    properties = schema.get("properties")
    if not properties:
        return schema
    for prop in properties.values():
        if isinstance(prop, dict) and prop.get("default", ...) is None:
            del prop["default"]
    return schema
