"""Runtime configuration, using the same environment variable names as the other
services so one compose file configures all five."""

import re
from functools import lru_cache

from pydantic import Field, computed_field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=".env", extra="ignore")

    server_port: int = Field(default=8085, alias="SERVER_PORT")

    db_host: str = Field(default="localhost", alias="DB_HOST")
    db_port: int = Field(default=5432, alias="DB_PORT")
    db_name: str = Field(default="offer_db", alias="DB_NAME")
    db_user: str = Field(default="postgres", alias="DB_USER")
    db_password: str = Field(default="postgres", alias="DB_PASSWORD")
    database_url_override: str | None = Field(default=None, alias="DATABASE_URL")

    # No defaults: a missing secret must stop startup rather than quietly leave the
    # service accepting unauthenticated traffic.
    jwt_secret: str = Field(alias="JWT_SECRET")
    internal_api_key: str = Field(alias="INTERNAL_API_KEY")

    seller_service_url: str = Field(default="http://localhost:8083", alias="SELLER_SERVICE_URL")
    request_service_url: str = Field(default="http://localhost:8084", alias="REQUEST_SERVICE_URL")
    # Only used to ask who the admins are, so a NEW_OFFER event can be addressed.
    auth_service_url: str = Field(default="http://localhost:8081", alias="AUTH_SERVICE_URL")

    # The broker notification events are published to. Unlike the secrets above it has a
    # working default: a missing broker costs a notification, not safety.
    rabbitmq_url: str = Field(default="amqp://guest:guest@localhost:5672/", alias="RABBITMQ_URL")

    http_timeout_seconds: float = 5.0

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
