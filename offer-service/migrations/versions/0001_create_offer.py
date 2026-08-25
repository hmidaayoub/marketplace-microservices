"""create offer table

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
        "offer",
        sa.Column(
            "offer_id",
            sa.dialects.postgresql.UUID(as_uuid=True),
            primary_key=True,
            server_default=sa.text("gen_random_uuid()"),
        ),
        sa.Column("seller_id", sa.dialects.postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("request_id", sa.dialects.postgresql.UUID(as_uuid=True), nullable=False),
        sa.Column("available_quantity", sa.Integer(), nullable=False),
        sa.Column("price_per_unit", sa.Numeric(12, 2), nullable=False),
        sa.Column("currency", sa.String(3), nullable=False),
        sa.Column("description", sa.String(), nullable=False, server_default=""),
        sa.Column("status", sa.String(16), nullable=False, server_default="PENDING"),
        sa.Column(
            "created_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        sa.Column(
            "updated_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.text("now()"),
        ),
        # The database is the last line of defence for the invariants the API also
        # checks, so a bad write cannot get in by another route.
        sa.CheckConstraint(
            "status IN ('PENDING', 'APPROVED', 'REJECTED', 'CANCELLED')", name="offer_status_valid"
        ),
        sa.CheckConstraint("available_quantity > 0", name="offer_quantity_positive"),
        sa.CheckConstraint("price_per_unit > 0", name="offer_price_positive"),
        sa.CheckConstraint("currency ~ '^[A-Z]{3}$'", name="offer_currency_iso4217_shape"),
    )

    op.create_index("idx_offer_request", "offer", ["request_id"])
    op.create_index("idx_offer_seller", "offer", ["seller_id"])
    op.create_index("idx_offer_status_created", "offer", ["status", "created_at"])


def downgrade() -> None:
    op.drop_index("idx_offer_status_created", table_name="offer")
    op.drop_index("idx_offer_seller", table_name="offer")
    op.drop_index("idx_offer_request", table_name="offer")
    op.drop_table("offer")
