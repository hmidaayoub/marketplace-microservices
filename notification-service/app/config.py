"""Runtime configuration, using the same environment variable names as the other
services so one compose file configures all six.

This service has no outbound service URLs, which is the point: it is told who to
notify and what to say, and never resolves an identity or reads business data of its
own (spec section 13 - "not the source of truth for business data").
"""

import re
from functools import lru_cache

from pydantic import Field, computed_field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    server_port: int = Field(default=8087, alias="SERVER_PORT")

    db_host: str = Field(default="localhost", alias="DB_HOST")
    db_port: int = Field(default=5432, alias="DB_PORT")
    db_name: str = Field(default="notification_db", alias="DB_NAME")
    db_user: str = Field(default="postgres", alias="DB_USER")
    db_password: str = Field(default="postgres", alias="DB_PASSWORD")
    database_url_override: str | None = Field(default=None, alias="DATABASE_URL")

    # No defaults: a missing secret must stop startup rather than quietly leave the
    # service accepting unauthenticated traffic.
    jwt_secret: str = Field(alias="JWT_SECRET")
    internal_api_key: str = Field(alias="INTERNAL_API_KEY")

    # The broker this service consumes notification events from. Unlike the secrets
    # above it has a working default: a missing broker degrades the service to its HTTP
    # path rather than leaving it insecure, so it must not stop startup.
    rabbitmq_url: str = Field(default="amqp://guest:guest@localhost:5672/", alias="RABBITMQ_URL")

    @computed_field
    @property
    def database_url(self) -> str:
        if self.database_url_override:
            # Accept any postgres URL - postgres://, postgresql://, or one naming a
            # different driver such as the postgresql+psycopg2:// that Testcontainers
            # hands out - and point it at the async driver this service uses.
            return re.sub(
                r"^post(?:gres|gresql)(?:\+\w+)?://",
                "postgresql+asyncpg://",
                self.database_url_override,
            )
        return (
            f"postgresql+asyncpg://{self.db_user}:{self.db_password}"
            f"@{self.db_host}:{self.db_port}/{self.db_name}"
        )


@lru_cache
def get_settings() -> Settings:
    return Settings()  # type: ignore[call-arg]
