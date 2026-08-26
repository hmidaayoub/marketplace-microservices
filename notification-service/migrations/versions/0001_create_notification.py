"""create notification table

Revision ID: 0001
Revises:
"""

import sqlalchemy as sa
from alembic import op

revision = "0001"
down_revision = None
branch_labels = None
depends_on = None


def upgrade() -> None:
    op.create_table(
        "notification",
        sa.Column(
            "notification_id",
            sa.dialects.postgresql.UUID(as_uuid=True),
            primary_key=True,
            server_default=sa.text("gen_random_uuid()"),
        ),
        sa.Column("user_id", sa.dialects.postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("type", sa.String(64), nullable=False),
        sa.Column("channel", sa.String(16), nullable=False, server_default="IN_APP"),
        sa.Column("title", sa.String(200), nullable=False),
        sa.Column("message", sa.String(), nullable=False),
        sa.Column("status", sa.String(16), nullable=False, server_default="PENDING"),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.Column("sent_at", sa.DateTime(timezone=True), nullable=True),
        # The database is the last line of defence for the invariants the API also
        # checks, so a bad write cannot get in by another route.
        sa.CheckConstraint(
            "status IN ('PENDING', 'SENT', 'FAILED', 'READ')", name="notification_status_valid"
        ),
        sa.CheckConstraint(
            "channel IN ('IN_APP', 'EMAIL', 'SMS', 'PUSH')", name="notification_channel_valid"
        ),
        # Open set, fixed shape: spec section 13 introduces the event list with
        # "Examples", so a new event must not require a migration here.
        sa.CheckConstraint("type ~ '^[A-Z][A-Z0-9_]*$'", name="notification_type_shape"),
        sa.CheckConstraint("length(btrim(title)) > 0", name="notification_title_not_blank"),
        # Keeps sent_at from drifting from status: a notification carries a send time
        # exactly when it has actually been sent.
        sa.CheckConstraint(
            "(status IN ('SENT', 'READ')) = (sent_at IS NOT NULL)",
            name="notification_sent_at_matches_status",
        ),
    )

    op.create_index("idx_notification_user_created", "notification", ["user_id", "created_at"])
    op.create_index("idx_notification_user_status", "notification", ["user_id", "status"])


def downgrade() -> None:
    op.drop_index("idx_notification_user_status", table_name="notification")
    op.drop_index("idx_notification_user_created", table_name="notification")
    op.drop_table("notification")
