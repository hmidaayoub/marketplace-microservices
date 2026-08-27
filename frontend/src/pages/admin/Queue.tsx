/**
 * The approval queue - the single gate in the platform.
 *
 * Approving is not a status change: it is what grants the seller access to the phone
 * numbers of every customer on that request. The copy says so, and the confirmation
 * says so again, because an admin clicking through a queue should know what the click
 * actually releases.
 */

import { useEffect, useState } from 'react'
import { CircleCheckIcon, CircleXIcon, GavelIcon, PhoneIcon } from 'lucide-react'
import { Link } from 'react-router-dom'
import { toast } from 'sonner'

import { ErrorAlert } from '@/components/error-alert'
import { PageHeader } from '@/components/page-header'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldLabel } from '@/components/ui/field'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import type { PendingOffer } from '@/api/types'
import { useAppDispatch, useAppSelector } from '@/store'
import { decideOffer, fetchPending } from '@/store/adminSlice'

export default function Queue() {
  const dispatch = useAppDispatch()
  const { pending, loading, error } = useAppSelector((s) => s.admin)
  const [reasons, setReasons] = useState<Record<string, string>>({})

  useEffect(() => {
    dispatch(fetchPending())
  }, [dispatch])

  const decide = async (offerId: string, approve: boolean) => {
    const result = await dispatch(
      decideOffer({
        offerId,
        approve,
        reason: reasons[offerId]?.trim() || (approve ? 'Approved' : 'Rejected'),
      }),
    )
    if (decideOffer.fulfilled.match(result)) {
      toast.success(approve ? 'Offer approved' : 'Offer rejected', {
        description: approve
          ? 'The seller can now see the phone numbers of the buyers on that request.'
          : 'The seller is notified. No contact details were released.',
      })
    }
  }

  return (
    <div className="space-y-6">
      <PageHeader
        title="Approval queue"
        description="Offers waiting on a decision. Yours is the only one in the platform that releases a phone number."
      />

      <Alert>
        <PhoneIcon />
        <AlertTitle>Approving releases contact details</AlertTitle>
        <AlertDescription>
          It grants the seller the phone numbers of every customer on that request, for that
          request only. Rejecting releases nothing.
        </AlertDescription>
      </Alert>

      <ErrorAlert>{error}</ErrorAlert>

      {loading && pending.length === 0 && (
        <div className="space-y-3">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} className="h-44 w-full rounded-xl" />
          ))}
        </div>
      )}

      {!loading && pending.length === 0 && (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <GavelIcon />
            </EmptyMedia>
            <EmptyTitle>Nothing is waiting for a decision</EmptyTitle>
            <EmptyDescription>New offers land here the moment a seller submits one.</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      <div className="space-y-3">
        {pending.map((offer) => (
          <PendingOfferCard
            key={offer.offerId}
            offer={offer}
            reason={reasons[offer.offerId ?? ''] ?? ''}
            onReason={(reason) =>
              setReasons((r) => ({ ...r, [offer.offerId ?? '']: reason }))
            }
            onDecide={(approve) => decide(offer.offerId!, approve)}
          />
        ))}
      </div>
    </div>
  )
}

function PendingOfferCard({
  offer,
  reason,
  onReason,
  onDecide,
}: {
  offer: PendingOffer
  reason: string
  onReason: (reason: string) => void
  onDecide: (approve: boolean) => void
}) {
  const reasonId = `reason-${offer.offerId}`

  return (
    <Card>
      <CardContent className="space-y-4">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="space-y-0.5">
            <p className="font-heading text-base font-medium tabular-nums">
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
          <span className="font-mono text-xs text-muted-foreground/70">{offer.offerId}</span>
        </div>

        <Field>
          <FieldLabel htmlFor={reasonId}>Reason (recorded with the decision)</FieldLabel>
          <Textarea
            id={reasonId}
            value={reason}
            rows={2}
            onChange={(e) => onReason(e.target.value)}
            placeholder="Best price for the aggregated demand"
          />
        </Field>

        <div className="flex flex-wrap gap-2">
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button>
                <CircleCheckIcon />
                Approve &amp; release
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Release the buyers' phone numbers?</AlertDialogTitle>
                <AlertDialogDescription>
                  Approving this offer grants the seller the phone number of every customer on
                  this request. The grant is recorded and can be revoked, but the numbers will
                  have been seen.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction onClick={() => onDecide(true)}>
                  Approve &amp; release
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>

          <Button variant="outline" onClick={() => onDecide(false)}>
            <CircleXIcon />
            Reject
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
