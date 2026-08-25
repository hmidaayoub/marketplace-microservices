"""Authentication for the two call styles the platform defines (spec section 6):
a user JWT on /api/**, and the shared internal key on /internal/**."""

import hmac
import logging
import uuid
from dataclasses import dataclass
from typing import Annotated

import jwt
from fastapi import Depends, Header, HTTPException, status

from app.config import Settings, get_settings

log = logging.getLogger(__name__)

INTERNAL_API_KEY_HEADER = "X-Internal-Api-Key"

ROLE_CUSTOMER = "CUSTOMER"
ROLE_SELLER = "SELLER"
ROLE_ADMIN = "ADMIN"

# auth-service signs with jjwt's signWith(SecretKey), which picks the HMAC variant from
# the key length - the deployed 48-byte secret yields HS384, not HS256. Pinning a single
# algorithm here would reject every real token the moment the secret length changes, so
# the whole HMAC family is accepted. Anything outside it is still refused, which is what
# closes off "alg": "none" and RS/HS confusion.
ALLOWED_ALGORITHMS = ["HS256", "HS384", "HS512"]


@dataclass(frozen=True)
class Principal:
    """The authenticated caller. Identity always comes from the token's sub claim;
    no handler may take it from a header or a request body."""

    user_id: uuid.UUID
    role: str
    email: str | None = None


def _unauthorized(detail: str = "Invalid token") -> HTTPException:
    return HTTPException(status_code=status.HTTP_401_UNAUTHORIZED, detail=detail)


def get_principal(
    settings: Annotated[Settings, Depends(get_settings)],
    authorization: Annotated[str | None, Header()] = None,
) -> Principal:
    if not authorization or not authorization.lower().startswith("bearer "):
        raise _unauthorized("Missing or malformed Authorization header")

    token = authorization[len("bearer ") :].strip()
    if not token:
        raise _unauthorized("Missing or malformed Authorization header")

    try:
        claims = jwt.decode(
            token,
            settings.jwt_secret,
            algorithms=ALLOWED_ALGORITHMS,
            options={"require": ["exp", "sub"]},
        )
    except jwt.PyJWTError as exc:
        log.warning("rejected token: %s", exc)
        raise _unauthorized() from exc

    # Refresh tokens are signed with the same key and differ only by this claim. They
    # must not authenticate an API call.
    if claims.get("type") == "refresh":
        raise _unauthorized("Refresh token cannot authenticate a request")

    try:
        user_id = uuid.UUID(claims["sub"])
    except (KeyError, ValueError) as exc:
        raise _unauthorized("Token subject is not a uuid") from exc

    role = claims.get("role")
    if not role:
        raise _unauthorized("Token has no role")

    return Principal(user_id=user_id, role=role, email=claims.get("email"))


CurrentPrincipal = Annotated[Principal, Depends(get_principal)]


def require_role(*roles: str):
    """Restrict a route to the given roles. Mounted behind get_principal, so an
    unauthenticated caller is already rejected before this runs."""

    allowed = frozenset(roles)

    def dependency(principal: CurrentPrincipal) -> Principal:
        if principal.role not in allowed:
            raise HTTPException(
                status_code=status.HTTP_403_FORBIDDEN,
                detail=f"Role {principal.role} may not perform this action",
            )
        return principal

    return dependency


def require_internal_api_key(
    settings: Annotated[Settings, Depends(get_settings)],
    x_internal_api_key: Annotated[str | None, Header()] = None,
) -> None:
    """Guards /internal/**. Fails closed on an unconfigured key and compares in
    constant time, since a plain == leaks the secret through response timing."""

    configured = settings.internal_api_key
    if not configured:
        log.error("INTERNAL_API_KEY is not configured - rejecting all /internal requests")
        raise _unauthorized("Unauthorized internal request")

    presented = x_internal_api_key or ""
    if not hmac.compare_digest(presented.encode(), configured.encode()):
        raise _unauthorized("Unauthorized internal request")
