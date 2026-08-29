import { useEffect } from 'react'
import { ContactIcon, ReceiptTextIcon } from 'lucide-react'
import { Link } from 'react-router-dom'

import { ErrorAlert } from '@/components/error-alert'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { useAppDispatch, useAppSelector } from '@/store'
import { fetchMyOffers } from '@/store/offersSlice'

export default function MyOffers() {
  const dispatch = useAppDispatch()
  const { mine, error } = useAppSelector((s) => s.offers)

  useEffect(() => {
    dispatch(fetchMyOffers())
  }, [dispatch])

  return (
    <div className="space-y-6">
      <PageHeader
        title="My offers"
        description="Every offer you have made. An administrator reviews each one — approval is also what releases the buyers' phone numbers to you."
      />

      <ErrorAlert>{error}</ErrorAlert>

      {mine.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ReceiptTextIcon />
            </EmptyMedia>
            <EmptyTitle>You have not made an offer yet</EmptyTitle>
            <EmptyDescription>
              Open requests show the combined demand of every buyer who joined — that is what you
              bid against. Nobody asking for what you sell is not a reason to wait: offer on the
              item anyway and the request opens with no buyers, ready for them to join.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button asChild>
              <Link to="/requests">Find a request</Link>
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <div className="space-y-3">
          {mine.map((offer) => (
            <Card key={offer.offerId} size="sm">
              <CardContent className="flex flex-wrap items-center justify-between gap-4">
                <div className="min-w-0 space-y-0.5">
                  <p className="font-medium tabular-nums">
                    {offer.availableQuantity} units · {offer.pricePerUnit} {offer.currency}
                  </p>
                  <p className="text-sm text-muted-foreground">{offer.description}</p>
                  <Link
                    to={`/requests/${offer.requestId}`}
                    className="text-sm font-medium text-primary hover:underline"
                  >
                    View the request
                  </Link>
                </div>

                <div className="flex shrink-0 items-center gap-2">
                  <StatusBadge status={offer.status ?? 'PENDING'} />
                  {offer.status === 'APPROVED' && (
                    <Button variant="outline" size="sm" asChild>
                      <Link to="/contacts">
                        <ContactIcon />
                        Contacts released
                      </Link>
                    </Button>
                  )}
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  )
}
