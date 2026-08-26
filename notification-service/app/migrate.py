"""Applies Alembic migrations at startup, so the schema a request sees is never
half-applied and the image carries its own schema."""

import logging
from pathlib import Path

from alembic import command
from alembic.config import Config

log = logging.getLogger(__name__)

_ROOT = Path(__file__).resolve().parent.parent


def run_migrations() -> None:
    config = Config(str(_ROOT / "alembic.ini"))
    config.set_main_option("script_location", str(_ROOT / "migrations"))
    command.upgrade(config, "head")
    log.info("database schema is up to date")
