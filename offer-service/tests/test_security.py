"""Token verification, including compatibility with the tokens auth-service actually
issues."""

import base64
import json
import uuid
from datetime import timedelta

import jwt
import pytest
from httpx import AsyncClient

from tests.conftest import TEST_SECRET, auth, make_token

# Minted by the real Java stack - common-security's jjwt, the dev secret below, through
# the same builder calls JwtUtil makes. Golden values rather than Python-generated
# lookalikes precisely because the thing under test is cross-language compatibility: a
# PyJWT-minted token would hide any jjwt-specific behaviour. The valid ones expire in 2035.
JAVA_ACCESS_TOKEN = "eyJhbGciOiJIUzM4NCJ9.eyJzdWIiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEiLCJlbWFpbCI6ImludGVyb3BAdGVzdC5jb20iLCJyb2xlIjoiQ1VTVE9NRVIiLCJpYXQiOjE3ODc2NzUyNjcsImV4cCI6MjEwMzAzNTI2N30.WJd6ItkA32ET7iND4BS520bFW0I7B6hW3Oql4c-Ndkwd8sS7A4m9MZ9SpbomtHLh"
JAVA_REFRESH_TOKEN = "eyJhbGciOiJIUzM4NCJ9.eyJzdWIiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEiLCJqdGkiOiI1ZmJiMDkwYS0yNDJiLTRjZjMtOTI4Ni0xYTA1ZjgxMGZkMDEiLCJ0eXBlIjoicmVmcmVzaCIsImlhdCI6MTc4NzY3NTI2NywiZXhwIjoyMTAzMDM1MjY3fQ.f50LI5fSM7Dbng6nbHwwwENyxHjIzFBO99gVdXdk4jNMEkNfA2e771NhFSoM-JHf"
JAVA_EXPIRED_TOKEN = "eyJhbGciOiJIUzM4NCJ9.eyJzdWIiOiIxMTExMTExMS0xMTExLTExMTEtMTExMS0xMTExMTExMTExMTEiLCJyb2xlIjoiQ1VTVE9NRVIiLCJpYXQiOjE3ODc2NjgwNjcsImV4cCI6MTc4NzY3MTY2N30.HlBEQ0_9jcCTSAFPj83Eiv---At9-H4UdAjtTAYFkMwhO0PVAcDRTw0DR8wp3jl1"

JAVA_USER_ID = "11111111-1111-1111-1111-111111111111"


def _header(token: str) -> dict:
    raw = token.split(".", 1)[0]
    raw += "=" * (-len(raw) % 4)
    return json.loads(base64.urlsafe_b64decode(raw))


def test_java_tokens_are_hs384():
    """The regression that matters: jjwt's signWith(SecretKey) selects the HMAC variant
    from the key size, so the 384-bit dev secret yields HS384. A verifier pinned to
    HS256 passes every Python-only test and then rejects every real token."""
    assert _header(JAVA_ACCESS_TOKEN) == {"alg": "HS384"}


async def test_accepts_a_real_java_access_token(client: AsyncClient, upstream):
    """A token from auth-service must open the API. It gets 403 (no seller profile
    registered with the stub) rather than 401 - which proves the token itself verified."""
    response = await client.get("/api/offers/me", headers=auth(JAVA_ACCESS_TOKEN))
    assert response.status_code == 403


async def test_java_refresh_token_cannot_authenticate(client: AsyncClient):
    """Signed with the same key, differing only by a type claim."""
    response = await client.get("/api/offers/me", headers=auth(JAVA_REFRESH_TOKEN))
    assert response.status_code == 401


async def test_expired_java_token_is_rejected(client: AsyncClient):
    response = await client.get("/api/offers/me", headers=auth(JAVA_EXPIRED_TOKEN))
    assert response.status_code == 401


async def test_token_signed_with_another_secret_is_rejected(client: AsyncClient):
    token = make_token(uuid.uuid4(), "SELLER", secret="a-different-secret-entirely-abcdef")
    response = await client.get("/api/offers/me", headers=auth(token))
    assert response.status_code == 401


async def test_alg_none_is_rejected(client: AsyncClient):
    """The classic algorithm-confusion attack: drop the signature and claim none."""
    token = jwt.encode({"sub": str(uuid.uuid4()), "role": "SELLER"}, key="", algorithm="none")
    response = await client.get("/api/offers/me", headers=auth(token))
    assert response.status_code == 401


async def test_expired_token_is_rejected(client: AsyncClient):
    token = make_token(uuid.uuid4(), "SELLER", expires_in=timedelta(minutes=-5))
    response = await client.get("/api/offers/me", headers=auth(token))
    assert response.status_code == 401


async def test_token_without_role_is_rejected(client: AsyncClient):
    token = jwt.encode(
        {"sub": str(uuid.uuid4()), "exp": 4102444800}, TEST_SECRET, algorithm="HS384"
    )
    response = await client.get("/api/offers/me", headers=auth(token))
    assert response.status_code == 401


async def test_token_with_non_uuid_subject_is_rejected(client: AsyncClient):
    token = jwt.encode(
        {"sub": "not-a-uuid", "role": "SELLER", "exp": 4102444800},
        TEST_SECRET,
        algorithm="HS384",
    )
    response = await client.get("/api/offers/me", headers=auth(token))
    assert response.status_code == 401


@pytest.mark.parametrize("header", ["", "Token abc", "Bearer", "Bearer    "])
async def test_malformed_authorization_header_is_rejected(client: AsyncClient, header: str):
    response = await client.get("/api/offers/me", headers={"Authorization": header})
    assert response.status_code == 401


@pytest.mark.parametrize("algorithm", ["HS256", "HS384", "HS512"])
async def test_whole_hmac_family_is_accepted(client: AsyncClient, upstream, algorithm: str):
    """The secret length decides the algorithm upstream, so all three must verify."""
    user_id, seller_id = uuid.uuid4(), uuid.uuid4()
    upstream.add_seller(user_id, seller_id)
    token = make_token(user_id, "SELLER", algorithm=algorithm)

    response = await client.get("/api/offers/me", headers=auth(token))

    assert response.status_code == 200
