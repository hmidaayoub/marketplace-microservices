"""an optional picture of what a seller is offering

The bytes live in their own table rather than in a column on offer. Every read of an
offer goes through the ORM model, so a LargeBinary column on it would be loaded by the
admin queue, the seller's own list and every competing-offer projection - a megabyte a
row, to render views that show none of it. Here nothing loads them but the endpoint that
serves the image.

What does live on the offer is the media type, because "is there a picture" is part of
reading an offer: the client needs it to decide whether to render an <img>, and it is
one small column rather than a join.

Mirrors request-service's 000010_request_image. Two services, one shape - a picture on a
request and a picture on an offer are the same thing seen from the two sides of the
same trade.

Revision ID: 0004
Revises: 0003
"""

import sqlalchemy as sa
from alembic import op

revision = "0004"
down_revision = "0003"
branch_labels = None
depends_on = None

# The three formats a browser renders without a plugin, a codec or a polyfill, plus the
# empty string for the offers - the overwhelming majority - carrying no picture at all.
#
# SVG is deliberately absent: it is a document that can carry script, and serving one
# from the platform's own origin would be a stored XSS against everyone who views it.
IMAGE_TYPES = "image_type IN ('', 'image/jpeg', 'image/png', 'image/webp')"

# The service caps uploads at 1 MiB, well below this. The constraint is here as well so
# a future caller that bypasses the handler cannot turn this table into blob storage.
SIZE_CAP = 2097152


def upgrade() -> None:
    op.add_column(
        "offer",
        sa.Column("image_type", sa.String(), nullable=False, server_default=""),
    )
    op.create_check_constraint("offer_image_type_valid", "offer", sa.text(IMAGE_TYPES))

    op.create_table(
        "offer_image",
        sa.Column(
            "offer_id",
            sa.dialects.postgresql.UUID(as_uuid=True),
            sa.ForeignKey("offer.offer_id", ondelete="CASCADE"),
            primary_key=True,
        ),
        sa.Column("image_data", sa.LargeBinary(), nullable=False),
        sa.Column(
            "updated_at",
            sa.DateTime(timezone=True),
            nullable=False,
            server_default=sa.func.now(),
        ),
        sa.CheckConstraint(f"length(image_data) <= {SIZE_CAP}", name="offer_image_within_size_cap"),
        sa.CheckConstraint("length(image_data) > 0", name="offer_image_not_empty"),
    )


def downgrade() -> None:
    op.drop_table("offer_image")
    op.drop_constraint("offer_image_type_valid", "offer", type_="check")
    op.drop_column("offer", "image_type")
