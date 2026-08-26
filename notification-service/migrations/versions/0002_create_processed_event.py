"""create processed_event table

Records which AMQP events have already been handled, so an at-least-once redelivery
cannot duplicate a user's notifications.

Revision ID: 0002
Revises: 0001
"""

import sqlalchemy as sa
from alembic import op

revision = "0002"
down_revision = "0001"
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "processed_event",
        # The producer's eventId, used as the primary key rather than a surrogate: the
        # uniqueness *is* the point, and a separate id would let a duplicate slip in.
        sa.Column("event_id", sa.dialects.postgresql.UUID(as_uuid=True), primary_key=True),
        sa.Column("source", sa.String(64), nullable=False),
        sa.Column(
            "processed_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
    )


def downgrade() -> None:
    op.drop_table("processed_event")
