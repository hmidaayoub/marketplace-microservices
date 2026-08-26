"""Offer Service - seller proposals against aggregated demand (spec section 11)."""

import asyncio
import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.responses import JSONResponse
from sqlalchemy import text

from app.clients import ServiceClients
from app.config import get_settings
from app.db import dispose_engine, get_engine
from app.errors import register_exception_handlers
from app.events import Publisher
from app.migrate import run_migrations
from app.routers import internal, offers


def configure_logging() -> None:
    """Installs the platform's log format on the root logger.

    Called twice, and both are needed. force=True because uvicorn installs its own root
    handler before importing this module, and basicConfig is a no-op when one is already
    there. Called again after migrations because Alembic's fileConfig reconfigures the
    root logger from alembic.ini - which sets it to WARNING - and would otherwise mute
    every application log line for the rest of the process, the background consumer's
    included.
    """
    logging.basicConfig(
        level=logging.INFO,
        format='{"level":"%(levelname)s","logger":"%(name)s","msg":"%(message)s"}',
        force=True,
    )


configure_logging()
log = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI):
    settings = get_settings()

    # Alembic is synchronous; run it off the event loop so startup does not block it.
    await asyncio.to_thread(run_migrations)
    configure_logging()  # Alembic just reset the root logger; take it back.

    app.state.clients = ServiceClients(
        seller_base_url=settings.seller_service_url,
        request_base_url=settings.request_service_url,
        auth_base_url=settings.auth_service_url,
        api_key=settings.internal_api_key,
        timeout=settings.http_timeout_seconds,
    )
    # The connection is opened on the first publish, not here: the service must start
    # whether or not the broker is up, and reconnect on its own if it restarts.
    app.state.publisher = Publisher(settings.rabbitmq_url, "offer-service")
    log.info("offer-service listening on port %s", settings.server_port)

    yield

    await app.state.publisher.aclose()
    await app.state.clients.aclose()
    await dispose_engine()


def create_app() -> FastAPI:
    app = FastAPI(
        title="Offer Service",
        version="0.1.0",
        description="Stores seller offers against purchase requests.",
        lifespan=lifespan,
    )

    register_exception_handlers(app)
    app.include_router(offers.router)
    app.include_router(internal.router)

    # /health is the idiomatic name; /actuator/health keeps probe configuration
    # identical across every service in the platform.
    @app.get("/health", tags=["health"])
    @app.get("/actuator/health", tags=["health"])
    async def health() -> JSONResponse:
        """Reports DOWN when the database is unreachable, so an unhealthy container is
        restarted rather than left accepting traffic it cannot serve."""
        try:
            async with get_engine().connect() as connection:
                await connection.execute(text("SELECT 1"))
        except Exception:
            log.exception("health check failed")
            return JSONResponse(status_code=503, content={"status": "DOWN"})
        return JSONResponse(status_code=200, content={"status": "UP"})

    return app


app = create_app()
