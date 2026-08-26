"""The OpenAPI document this service publishes.

Bearer auth is declared here rather than by swapping the token dependency for FastAPI's
HTTPBearer: the dependency reads the header itself and raises the platform's own 401,
and changing it to get a nicer document would change behaviour the tests pin down. This
is a documentation concern, so it is solved in the document.
"""

from typing import Any

from fastapi import FastAPI
from fastapi.openapi.utils import get_openapi

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
