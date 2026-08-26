import { useEffect } from 'react'
import { Link } from 'react-router-dom'

import { useAppDispatch, useAppSelector } from '../store'
import { fetchMyOffers } from '../store/offersSlice'
import { Alert, Badge, Card, Empty } from '../components/ui'

export default function MyOffers() {
  const dispatch = useAppDispatch()
  const { mine, error } = useAppSelector((s) => s.offers)

  useEffect(() => {
    dispatch(fetchMyOffers())
  }, [dispatch])

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">My offers</h1>
      <Alert>{error}</Alert>
      {mine.length === 0 && <Empty>You have not made an offer yet.</Empty>}
      {mine.map((offer) => (
        <Card key={offer.offerId} className="flex items-center justify-between gap-4">
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
          <div className="text-right">
            <Badge>{offer.status ?? 'PENDING'}</Badge>
            {offer.status === 'APPROVED' && (
              <p className="mt-1 text-xs text-emerald-700">
                <Link to="/contacts" className="hover:underline">
                  Contacts released →
                </Link>
              </p>
            )}
          </div>
        </Card>
      ))}
    </div>
  )
}
