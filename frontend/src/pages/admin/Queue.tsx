/**
 * The approval queue - the single gate in the platform.
 *
 * Approving is not a status change: it is what grants the seller access to the phone
 * numbers of every customer on that request. The copy says so, because an admin
 * clicking through a queue should know what the click actually releases.
 */

import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import { useAppDispatch, useAppSelector } from '../../store'
import { decideOffer, fetchPending } from '../../store/adminSlice'
import { Alert, Button, Card, Empty, Field, Input } from '../../components/ui'

export default function Queue() {
  const dispatch = useAppDispatch()
  const { pending, loading, error } = useAppSelector((s) => s.admin)
  const [reasons, setReasons] = useState<Record<string, string>>({})

  useEffect(() => {
    dispatch(fetchPending())
  }, [dispatch])

  const decide = (offerId: string, approve: boolean) =>
    dispatch(
      decideOffer({
        offerId,
        approve,
        reason: reasons[offerId]?.trim() || (approve ? 'Approved' : 'Rejected'),
      }),
    )

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Approval queue</h1>
      <p className="text-sm text-slate-600">
        Approving an offer releases the customers' phone numbers to that seller, for that request
        only.
      </p>

      <Alert>{error}</Alert>
      {loading && <Empty>Loading…</Empty>}
      {!loading && pending.length === 0 && <Empty>Nothing is waiting for a decision.</Empty>}

      {pending.map((offer) => (
        <Card key={offer.offerId} className="space-y-3">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="font-medium">
                {offer.availableQuantity} units · {offer.pricePerUnit} {offer.currency}
              </p>
              <p className="text-sm text-slate-600">{offer.description}</p>
              <Link
                to={`/requests/${offer.requestId}`}
                className="text-sm text-brand-600 hover:underline"
              >
                View the request
              </Link>
            </div>
            <span className="text-xs text-slate-400">{offer.offerId}</span>
          </div>

          <div className="flex flex-wrap items-end gap-3">
            <div className="min-w-64 flex-1">
              <Field label="Reason (recorded with the decision)">
                <Input
                  value={reasons[offer.offerId ?? ''] ?? ''}
                  onChange={(e) =>
                    setReasons((r) => ({ ...r, [offer.offerId ?? '']: e.target.value }))
                  }
                  placeholder="Best price for the aggregated demand"
                />
              </Field>
            </div>
            <Button onClick={() => decide(offer.offerId!, true)}>Approve &amp; release</Button>
            <Button variant="ghost" onClick={() => decide(offer.offerId!, false)}>
              Reject
            </Button>
          </div>
        </Card>
      ))}
    </div>
  )
}
