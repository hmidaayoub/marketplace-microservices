/**
 * One request: its aggregated demand, the offers against it, and whatever the caller's
 * role lets them do with it.
 *
 * The offers list is where the platform's role projection shows: a rival seller gets
 * CompetingOfferOut with the sellerId withheld, everyone else the full offer. The two
 * shapes are told apart by isFullOffer rather than by guessing from the caller's role.
 */

import { useEffect, useState } from 'react'
import {
  ArrowLeftIcon,
  EyeOffIcon,
  HandshakeIcon,
  LockIcon,
  LogInIcon,
  LogOutIcon,
  PackageIcon,
  TagIcon,
  UsersIcon,
} from 'lucide-react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { toast } from 'sonner'

import { ErrorAlert } from '@/components/error-alert'
import { StatusBadge } from '@/components/status-badge'
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
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import type { Customer } from '@/api/types'
import { isFullOffer } from '@/api/types'
import { useAppDispatch, useAppSelector } from '@/store'
import { createOffer, fetchOffersForRequest } from '@/store/offersSlice'
import {
  closeRequest,
  fetchMyRequests,
  fetchRequest,
  joinRequest,
  leaveRequest,
  updateQuantity,
} from '@/store/requestsSlice'

export default function RequestDetail() {
  const { id = '' } = useParams()
  const dispatch = useAppDispatch()
  const request = useAppSelector((s) => s.requests.current)
  const requestError = useAppSelector((s) => s.requests.error)
  const offers = useAppSelector((s) => s.offers.forRequest)
  const offerError = useAppSelector((s) => s.offers.error)
  const user = useAppSelector((s) => s.auth.user)
  const signedIn = useAppSelector((s) => Boolean(s.auth.accessToken))
  const mine = useAppSelector((s) => s.requests.mine)
  const profile = useAppSelector((s) => s.auth.profile)

  useEffect(() => {
    dispatch(fetchRequest(id))
    // The request itself reads publicly; the offers on it do not. Their projection
    // depends on who is asking - a rival seller gets the sellerId withheld - so there is
    // no anonymous shape for it, and asking would only earn a 401.
    if (signedIn) dispatch(fetchOffersForRequest(id))
    // Nothing on the request itself says whether the caller is on it - the response is
    // the same for every reader - so participation is read from the caller's own list.
    if (user?.role === 'CUSTOMER') dispatch(fetchMyRequests())
  }, [dispatch, id, signedIn, user?.role])

  if (!request) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-24 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  // createdBy is the customer who opened the request, not the account behind them -
  // request-service stores the customerId it resolved the caller to. Comparing it to
  // the userId matched nothing, so the owner was never offered Close request.
  const customerId = user?.role === 'CUSTOMER' ? (profile as Customer | null)?.customerId : undefined
  const isOwner = Boolean(customerId) && request.createdBy === customerId
  const isOpen = request.status === 'OPEN'
  // The creator is added as a participant when the request is made, so an owner is
  // always joined and never sees Join either.
  const hasJoined = mine.some((r) => r.requestId === id)

  return (
    <div className="space-y-6">
      <Button variant="ghost" size="sm" asChild className="-ml-2.5 text-muted-foreground">
        <Link to="/requests">
          <ArrowLeftIcon />
          Back to open demand
        </Link>
      </Button>

      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="space-y-1">
          <h1 className="font-heading text-2xl font-semibold tracking-tight">
            {request.itemName}
          </h1>
          <p className="max-w-2xl text-sm text-muted-foreground">{request.description}</p>
        </div>
        <StatusBadge status={request.status ?? 'OPEN'} />
      </div>

      <div className="grid gap-4 sm:grid-cols-3">
        <Stat icon={<UsersIcon />} label="Buyers" value={request.totalCustomers} />
        <Stat icon={<PackageIcon />} label="Units wanted" value={request.totalQuantity} />
        <Stat icon={<TagIcon />} label="Category" value={request.category} />
      </div>

      <ErrorAlert>{requestError || offerError}</ErrorAlert>

      {user?.role === 'CUSTOMER' && isOpen && (
        <Participation id={id} isOwner={isOwner} hasJoined={hasJoined} />
      )}
      {user?.role === 'SELLER' && isOpen && <OfferForm requestId={id} />}
      {!signedIn && isOpen && <SignInToTakePart />}

      <section className="space-y-3">
        <h2 className="font-heading text-lg font-medium">
          Offers{signedIn ? ` (${offers.length})` : ''}
        </h2>

        {!signedIn ? (
          <Empty className="border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <EyeOffIcon />
              </EmptyMedia>
              <EmptyTitle>Offers are visible once you sign in</EmptyTitle>
              <EmptyDescription>
                Who has bid, and for how much, is shown differently depending on who is
                asking — a rival seller does not get to see whose offer is whose.
              </EmptyDescription>
            </EmptyHeader>
            <EmptyContent>
              <SignInButtons />
            </EmptyContent>
          </Empty>
        ) : offers.length === 0 ? (
          <Empty className="border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <HandshakeIcon />
              </EmptyMedia>
              <EmptyTitle>No offers yet</EmptyTitle>
              <EmptyDescription>
                Sellers bid against the combined demand. More buyers makes this request worth
                bidding on.
              </EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="space-y-3">
            {offers.map((offer) => (
              <Card key={offer.offerId} size="sm">
                <CardContent className="flex items-center justify-between gap-4">
                  <div className="min-w-0 space-y-0.5">
                    <p className="font-medium tabular-nums">
                      {offer.availableQuantity} units · {offer.pricePerUnit} {offer.currency}
                    </p>
                    {isFullOffer(offer) ? (
                      <p className="truncate text-sm text-muted-foreground">{offer.description}</p>
                    ) : (
                      <p className="flex items-center gap-1.5 text-sm text-muted-foreground italic">
                        <EyeOffIcon className="size-3.5" />
                        Competing offer — the seller is not disclosed
                      </p>
                    )}
                  </div>
                  <StatusBadge status={offer.status ?? 'PENDING'} />
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </section>
    </div>
  )
}

/** Both ways in, carrying the page the visitor is standing on. */
function SignInButtons() {
  const location = useLocation()
  return (
    <div className="flex flex-wrap gap-2">
      <Button asChild>
        <Link to="/login" state={{ from: location }}>
          <LogInIcon />
          Sign in
        </Link>
      </Button>
      <Button variant="outline" asChild>
        <Link to="/register">Create an account</Link>
      </Button>
    </div>
  )
}

function SignInToTakePart() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Join this request, or offer against it</CardTitle>
        <CardDescription>
          Buyers add their quantity to the total; sellers bid against that total. Both need an
          account — looking does not.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <SignInButtons />
      </CardContent>
    </Card>
  )
}

function Stat({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode
  label: string
  value: React.ReactNode
}) {
  return (
    <Card size="sm">
      <CardContent className="space-y-1">
        <p className="flex items-center gap-1.5 text-xs text-muted-foreground [&_svg]:size-3.5">
          {icon}
          {label}
        </p>
        <p className="font-heading text-2xl font-semibold tabular-nums">{value}</p>
      </CardContent>
    </Card>
  )
}

function Participation({
  id,
  isOwner,
  hasJoined,
}: {
  id: string
  isOwner: boolean
  hasJoined: boolean
}) {
  const navigate = useNavigate()
  const dispatch = useAppDispatch()
  const [quantity, setQuantity] = useState(1)

  // Every one of these thunks reports failure by rejecting into the slice's error, which
  // is already on screen - so the toast is only for the success the page cannot show.
  // after runs only on success, so a refused join leaves the caller on the request
  // rather than sending them to a list they are not on.
  const run = async (
    dispatched: Promise<{ type: string }>,
    message: string,
    after?: () => void,
  ) => {
    const result = await dispatched
    if (result.type.endsWith('/rejected')) return
    toast.success(message)
    after?.()
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Your participation</CardTitle>
        <CardDescription>
          {hasJoined
            ? 'You are on this request. Change the quantity you asked for, or leave it.'
            : 'Joining adds your quantity to the total.'}
          {isOwner && ' Closing notifies every participant.'}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-wrap items-end gap-3">
        <Field className="w-32">
          <FieldLabel htmlFor="quantity">Quantity</FieldLabel>
          <Input
            id="quantity"
            type="number"
            min={1}
            value={quantity}
            onChange={(e) => setQuantity(Number(e.target.value))}
          />
        </Field>

        {/* Joining is only on offer to someone who is not already on the request:
            the API answers a second join with a 409, and an action whose only outcome
            is that error is not one to put in front of them. */}
        {hasJoined ? (
          <>
            <Button
              variant="outline"
              onClick={() => run(dispatch(updateQuantity({ id, quantity })), 'Quantity updated')}
            >
              Update my quantity
            </Button>
            {/* Leaving ends the same place joining does: the list of what you are on,
                which this request has just dropped off. Staying here would leave the
                caller looking at a request whose only remaining action is to rejoin. */}
            <Button
              variant="ghost"
              onClick={() =>
                run(dispatch(leaveRequest(id)), 'You left this request', () =>
                  navigate('/my-requests'),
                )
              }
            >
              <LogOutIcon />
              Leave
            </Button>
          </>
        ) : (
          <Button
            onClick={() =>
              run(dispatch(joinRequest({ id, quantity })), 'Joined this request', () =>
                navigate('/my-requests'),
              )
            }
          >
            Join
          </Button>
        )}

        {isOwner && (
          <AlertDialog>
            <AlertDialogTrigger asChild>
              <Button variant="destructive" className="ml-auto">
                <LockIcon />
                Close request
              </Button>
            </AlertDialogTrigger>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>Close this request?</AlertDialogTitle>
                <AlertDialogDescription>
                  Every participant is notified, and no further offers can be made against it.
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Keep it open</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() => run(dispatch(closeRequest(id)), 'Request closed')}
                >
                  Close request
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        )}
      </CardContent>
    </Card>
  )
}

function OfferForm({ requestId }: { requestId: string }) {
  const dispatch = useAppDispatch()
  const [saving, setSaving] = useState(false)
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
    setSaving(true)
    const result = await dispatch(createOffer({ requestId, ...form }))
    setSaving(false)
    if (createOffer.fulfilled.match(result)) {
      dispatch(fetchOffersForRequest(requestId))
      toast.success('Offer submitted', { description: 'An administrator reviews it next.' })
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Make an offer</CardTitle>
        <CardDescription>
          An administrator reviews every offer. Contact details are released only if yours is
          approved.
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit}>
          <FieldGroup>
            <div className="grid gap-5 sm:grid-cols-3">
              <Field>
                <FieldLabel htmlFor="availableQuantity">Units you can supply</FieldLabel>
                <Input
                  id="availableQuantity"
                  type="number"
                  min={1}
                  value={form.availableQuantity}
                  onChange={set('availableQuantity')}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="pricePerUnit">Price per unit</FieldLabel>
                <Input
                  id="pricePerUnit"
                  value={form.pricePerUnit}
                  onChange={set('pricePerUnit')}
                  required
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="currency">Currency</FieldLabel>
                <Input
                  id="currency"
                  value={form.currency}
                  onChange={set('currency')}
                  maxLength={3}
                  required
                />
              </Field>
            </div>

            <Field>
              <FieldLabel htmlFor="offerDescription">Notes</FieldLabel>
              <Textarea
                id="offerDescription"
                value={form.description}
                onChange={set('description')}
                rows={2}
              />
              <FieldDescription>
                What the buyers get for that price — lead time, condition, warranty.
              </FieldDescription>
            </Field>

            <div>
              <Button type="submit" disabled={saving}>
                {saving && <Spinner />}
                Submit offer
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}
