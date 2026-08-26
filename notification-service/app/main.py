"""Notification Service - records and delivers notifications (spec section 13)."""

import asyncio
import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from fastapi.responses import JSONResponse
from sqlalchemy import text

from app.config import get_settings
from app.db import dispose_engine, get_engine
from app.errors import register_exception_handlers
from app.events import Consumer
from app.migrate import run_migrations
from app.routers import internal, notifications


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

    # No ServiceClients here, unlike every other service: this one is told who to
    # notify and never has to ask. The only connection it opens is to the broker.
    #
    # start() returns immediately and the consumer connects in the background, so a
    # broker that is slow or down cannot stop this service from starting. The inbox is
    # a plain database read and has no reason to be unavailable because messaging is.
    app.state.consumer = Consumer(settings.rabbitmq_url)
    app.state.consumer.start()

    log.info("notification-service listening on port %s", settings.server_port)

    yield

    await app.state.consumer.stop()
    await dispose_engine()


def create_app() -> FastAPI:
    app = FastAPI(
        title="Notification Service",
        version="0.1.0",
        description="Records and delivers notifications to users.",
        lifespan=lifespan,
    )

    register_exception_handlers(app)
    app.include_router(notifications.router)
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
