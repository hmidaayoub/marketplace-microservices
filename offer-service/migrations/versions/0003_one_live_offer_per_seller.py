"""one live offer per seller per request

A seller answers a request once. Bidding twice against the same demand is not two
proposals, it is one proposal changed - so the second submission is refused and the
first is what gets updated (PUT /api/offers/{offerId}), which is the call that already
exists for exactly that.

Live means PENDING or APPROVED. The other two statuses are records rather than
proposals: a CANCELLED offer was withdrawn and a REJECTED one was refused, neither can
be updated - `_own_pending_offer` freezes everything that is not PENDING - so blocking a
fresh offer on the strength of one would lock the seller out of the request for good with
nothing to update instead.

Enforced by the index and not only in the service, for the same reason
request_participant carries a unique constraint: two concurrent submissions would both
pass a read-then-write check and both insert.

Revision ID: 0003
Revises: 0002
"""

import sqlalchemy as sa
from alembic import op

revision = "0003"
down_revision = "0002"
branch_labels = None
depends_on = None

LIVE = "status IN ('PENDING', 'APPROVED')"


def upgrade() -> None:
    # Nothing stopped a second offer until now, so the index cannot be built over the
    # rows as they stand. The newest live offer per (seller, request) is kept - it is the
    # one the seller meant last - and the others are cancelled rather than deleted, which
    # is what cancelling has always meant here: the record survives for the audit history
    # Admin/Contact refers back to.
    #
    # An APPROVED offer is never the one cancelled. Contact permission may already have
    # been granted against it, and withdrawing it here would revoke by side effect
    # something an admin decided deliberately.
    op.execute(f"""
        UPDATE offer
        SET status = 'CANCELLED', updated_at = now()
        WHERE status = 'PENDING'
          AND offer_id <> (
              SELECT keep.offer_id FROM offer keep
              WHERE keep.seller_id = offer.seller_id
                AND keep.request_id = offer.request_id
                AND {LIVE}
              ORDER BY (keep.status = 'APPROVED') DESC, keep.created_at DESC, keep.offer_id
              LIMIT 1
          )
    """)

    op.create_index(
        "idx_offer_one_live_per_seller_request",
        "offer",
        ["seller_id", "request_id"],
        unique=True,
        postgresql_where=sa.text(LIVE),
    )


def downgrade() -> None:
    op.drop_index("idx_offer_one_live_per_seller_request", table_name="offer")
