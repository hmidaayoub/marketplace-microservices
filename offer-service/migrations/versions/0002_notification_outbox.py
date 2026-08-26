"""create notification_outbox table

Holds notification events until a relay publishes them, written in the same transaction
as the offer that caused them so a broker outage delays a notification instead of
losing it. See docs/events.md.

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
        "notification_outbox",
        sa.Column(
            "outbox_id",
            sa.dialects.postgresql.UUID(as_uuid=True),
            primary_key=True,
            server_default=sa.text("gen_random_uuid()"),
        ),
        # Fixed at enqueue time, not at publish time: a relay that republishes a row
        # after dying mid-commit sends the same id, which the consumer recognises as a
        # redelivery rather than a second notification.
        sa.Column(
            "event_id", sa.dialects.postgresql.UUID(as_uuid=True), nullable=False, unique=True
        ),
        sa.Column("routing_key", sa.String(), nullable=False),
        sa.Column("payload", sa.dialects.postgresql.JSONB(), nullable=False),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.Column("published_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("attempts", sa.Integer(), nullable=False, server_default="0"),
        sa.Column("last_error", sa.String(), nullable=True),
        sa.CheckConstraint(
            "length(btrim(routing_key)) > 0", name="notification_outbox_routing_key_not_blank"
        ),
    )

    # The relay's only query. Partial, so it stays small however much history builds up.
    op.create_index(
        "idx_notification_outbox_pending",
        "notification_outbox",
        ["created_at"],
        postgresql_where=sa.text("published_at IS NULL"),
    )


def downgrade() -> None:
    op.drop_index("idx_notification_outbox_pending", table_name="notification_outbox")
    op.drop_table("notification_outbox")
