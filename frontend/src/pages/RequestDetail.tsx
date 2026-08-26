/**
 * One request: its aggregated demand, the offers against it, and whatever the caller's
 * role lets them do with it.
 *
 * The offers list is where the platform's role projection shows: a rival seller gets
 * CompetingOfferOut with the sellerId withheld, everyone else the full offer. The two
 * shapes are told apart by isFullOffer rather than by guessing from the caller's role.
 */

import { useEffect, useState } from 'react'
import { useParams } from 'react-router-dom'

import { isFullOffer } from '../api/types'
import { useAppDispatch, useAppSelector } from '../store'
import { createOffer, fetchOffersForRequest } from '../store/offersSlice'
import {
  closeRequest,
  fetchRequest,
  joinRequest,
  leaveRequest,
  updateQuantity,
} from '../store/requestsSlice'
import { Alert, Badge, Button, Card, Empty, Field, Input } from '../components/ui'

export default function RequestDetail() {
  const { id = '' } = useParams()
  const dispatch = useAppDispatch()
  const request = useAppSelector((s) => s.requests.current)
  const requestError = useAppSelector((s) => s.requests.error)
  const offers = useAppSelector((s) => s.offers.forRequest)
  const offerError = useAppSelector((s) => s.offers.error)
  const user = useAppSelector((s) => s.auth.user)
  const [quantity, setQuantity] = useState(1)

  useEffect(() => {
    dispatch(fetchRequest(id))
    dispatch(fetchOffersForRequest(id))
  }, [dispatch, id])

  if (!request) return <Empty>Loading…</Empty>

  const isOwner = request.createdBy === user?.userId
  const isOpen = request.status === 'OPEN'

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-semibold">{request.itemName}</h1>
          <p className="text-slate-600">{request.description}</p>
        </div>
        <Badge>{request.status ?? 'OPEN'}</Badge>
      </div>

      <Card className="flex gap-8">
        <div>
          <p className="text-sm text-slate-500">Buyers</p>
          <p className="text-2xl font-semibold">{request.totalCustomers}</p>
        </div>
        <div>
          <p className="text-sm text-slate-500">Units wanted</p>
          <p className="text-2xl font-semibold">{request.totalQuantity}</p>
        </div>
        <div>
          <p className="text-sm text-slate-500">Category</p>
          <p className="text-2xl font-semibold">{request.category}</p>
        </div>
      </Card>

      <Alert>{requestError || offerError}</Alert>

      {user?.role === 'CUSTOMER' && isOpen && (
        <Card className="space-y-3">
          <h2 className="font-medium">Your participation</h2>
          <div className="flex flex-wrap items-end gap-3">
            <div className="w-40">
              <Field label="Quantity">
                <Input
                  type="number"
                  min={1}
                  value={quantity}
                  onChange={(e) => setQuantity(Number(e.target.value))}
                />
              </Field>
            </div>
            <Button onClick={() => dispatch(joinRequest({ id, quantity }))}>Join</Button>
            <Button variant="ghost" onClick={() => dispatch(updateQuantity({ id, quantity }))}>
              Update my quantity
            </Button>
            <Button variant="ghost" onClick={() => dispatch(leaveRequest(id))}>
              Leave
            </Button>
            {isOwner && (
              <Button variant="danger" onClick={() => dispatch(closeRequest(id))}>
                Close request
              </Button>
            )}
          </div>
          <p className="text-xs text-slate-500">
            Joining adds your quantity to the total. Closing notifies every participant.
          </p>
        </Card>
      )}

      {user?.role === 'SELLER' && isOpen && <OfferForm requestId={id} />}

      <section className="space-y-2">
        <h2 className="text-lg font-medium">Offers ({offers.length})</h2>
        {offers.length === 0 && <Empty>No offers against this request yet.</Empty>}
        {offers.map((offer) => (
          <Card key={offer.offerId} className="flex items-center justify-between gap-4">
            <div>
              <p className="font-medium">
                {offer.availableQuantity} units · {offer.pricePerUnit} {offer.currency}
              </p>
              <p className="text-sm text-slate-600">
                {isFullOffer(offer) ? (
                  offer.description
                ) : (
                  <span className="italic text-slate-400">
                    Competing offer — the seller is not disclosed
                  </span>
                )}
              </p>
            </div>
            <Badge>{offer.status ?? 'PENDING'}</Badge>
          </Card>
        ))}
      </section>
    </div>
  )
}

function OfferForm({ requestId }: { requestId: string }) {
  const dispatch = useAppDispatch()
  const [form, setForm] = useState({
    availableQuantity: 1,
    pricePerUnit: '0.00',
    currency: 'EUR',
    description: '',
  })

  const set = (key: keyof typeof form) => (event: { target: { value: string } }) =>
    setForm((f) => ({
      ...f,
      [key]: key === 'availableQuantity' ? Number(event.target.value) : event.target.value,
    }))

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    const result = await dispatch(createOffer({ requestId, ...form }))
    if (createOffer.fulfilled.match(result)) dispatch(fetchOffersForRequest(requestId))
  }

  return (
    <Card>
      <h2 className="mb-3 font-medium">Make an offer</h2>
      <form onSubmit={submit} className="grid gap-3 sm:grid-cols-4">
        <Field label="Units you can supply">
          <Input
            type="number"
            min={1}
            value={form.availableQuantity}
            onChange={set('availableQuantity')}
          />
        </Field>
        <Field label="Price per unit">
          <Input value={form.pricePerUnit} onChange={set('pricePerUnit')} required />
        </Field>
        <Field label="Currency">
          <Input value={form.currency} onChange={set('currency')} maxLength={3} required />
        </Field>
        <div className="flex items-end">
          <Button type="submit">Submit offer</Button>
        </div>
        <div className="sm:col-span-4">
          <Field label="Notes">
            <Input value={form.description} onChange={set('description')} />
          </Field>
        </div>
        <p className="text-xs text-slate-500 sm:col-span-4">
          An administrator reviews every offer. Contact details are released only if yours is
          approved.
        </p>
      </form>
    </Card>
  )
}
