/** Open demand. Any authenticated role may browse - that is how a seller finds work. */

import { useEffect, useState } from 'react'
import { HandshakeIcon, LogInIcon, PackageIcon, PlusIcon, SearchIcon, UsersIcon } from 'lucide-react'
import { Link, useLocation, useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { ErrorAlert } from '@/components/error-alert'
import { PageHeader } from '@/components/page-header'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import type { PurchaseRequest } from '@/api/types'
import { useAppDispatch, useAppSelector } from '@/store'
import { createOffer } from '@/store/offersSlice'
import {
  createRequest,
  fetchRequests,
  fetchSimilarRequests,
  joinRequest,
  similarCleared,
} from '@/store/requestsSlice'

export default function BrowseRequests() {
  const dispatch = useAppDispatch()
  const { browse, loading, error } = useAppSelector((s) => s.requests)
  const role = useAppSelector((s) => s.auth.user?.role)
  // The token, not the role: mid session-restore there is a token but no role yet, and
  // flashing "sign in" at somebody who already is would be worse than showing nothing.
  const signedIn = useAppSelector((s) => Boolean(s.auth.accessToken))
  const location = useLocation()
  const [query, setQuery] = useState('')
  const searching = query.trim().length > 0

  useEffect(() => {
    // Typing should not fire a request per keystroke; the pause is short enough that
    // the list still feels live.
    const id = setTimeout(() => {
      dispatch(
        fetchRequests({
          q: query.trim() || undefined,
          // Browsing shows live demand only. A dormant request - nobody wants the item
          // and nobody is selling it - is not something to put in front of someone who
          // came to see what the platform has: there is nothing happening on it.
          //
          // Searching is the exception, and it has to be. The platform allows one
          // request per item, so somebody typing "Espresso Machine" has to be able to
          // find the dormant one and join it - otherwise they would be told the name is
          // taken by a request they were never shown. So a search drops the filter and
          // looks at every status.
          status: searching ? undefined : 'OPEN',
        }),
      )
    }, 250)
    return () => clearTimeout(id)
  }, [dispatch, query, searching])

  return (
    <div className="space-y-6">
      <PageHeader
        title="Open demand"
        description="Every request buyers have pooled. Sellers bid against the combined total, not one buyer's quantity."
      >
        {role === 'CUSTOMER' && <NewRequestDialog />}
        {role === 'SELLER' && <OfferOnANewItemDialog />}

        {/* Anyone may look; posting needs an account. The button is offered rather than
            hidden, because "you cannot do this yet" is more useful than a missing
            control - and it carries the way back here. */}
        {!signedIn && (
          <Button size="lg" asChild>
            <Link to="/login" state={{ from: location }}>
              <LogInIcon />
              Sign in to post a request
            </Link>
          </Button>
        )}
      </PageHeader>

      <Card size="sm">
        <CardContent>
          <div className="relative">
            <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              placeholder="Search for an espresso machine…"
              aria-label="Search requests"
              className="pl-8"
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
        </CardContent>
      </Card>

      <ErrorAlert>{error}</ErrorAlert>

      {loading && browse.length === 0 && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 6 }, (_, i) => (
            <Card key={i}>
              <CardHeader>
                <Skeleton className="h-4 w-2/3" />
                <Skeleton className="h-3 w-full" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-8 w-1/2" />
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {!loading && browse.length === 0 && (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <SearchIcon />
            </EmptyMedia>
            <EmptyTitle>
              {searching ? 'Nothing matches that search' : 'No open demand yet'}
            </EmptyTitle>
            <EmptyDescription>
              {role === 'SELLER'
                ? 'If nobody has asked for what you sell, offer on it anyway — the request opens with no buyers and they join it.'
                : searching
                  ? 'Nothing carries that name, dormant requests included. Open a request for it and other buyers can join you.'
                  : 'Nobody is asking for anything at the moment. Open the first request and sellers will bid against it.'}
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {browse.map((request) => (
          <RequestCard key={request.requestId} request={request} />
        ))}
      </div>
    </div>
  )
}

function RequestCard({ request }: { request: PurchaseRequest }) {
  return (
    <Link
      to={`/requests/${request.requestId}`}
      className="group rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      <Card className="h-full transition-shadow group-hover:ring-primary/40 group-hover:shadow-md">
        <CardHeader>
          <div className="flex items-start justify-between gap-2">
            <h2 className="font-heading font-medium">{request.itemName}</h2>
            <StatusBadge status={request.status ?? 'OPEN'} />
          </div>
          <CardDescription className="line-clamp-2">{request.description}</CardDescription>
        </CardHeader>
        <CardContent className="flex gap-6">
          <div>
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <UsersIcon className="size-3.5" />
              Buyers
            </p>
            <p className="font-heading text-lg font-semibold tabular-nums">
              {request.totalCustomers}
            </p>
          </div>
          <div>
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <PackageIcon className="size-3.5" />
              Units wanted
            </p>
            <p className="font-heading text-lg font-semibold tabular-nums">
              {request.totalQuantity}
            </p>
          </div>
          {/* The other half of what the status means: a request with no buyers but an
              offer standing on it is not dormant. It also tells a seller what they are
              bidding against before they open the request. */}
          <div>
            <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <HandshakeIcon className="size-3.5" />
              Offers
            </p>
            <p className="font-heading text-lg font-semibold tabular-nums">
              {request.totalOffers ?? 0}
            </p>
          </div>
        </CardContent>
      </Card>
    </Link>
  )
}

/**
 * A seller offering on something nobody has asked for yet.
 *
 * Demand and supply do not have to arrive in that order. Naming the item is enough: the
 * request is opened as part of submitting the offer, with no buyers on it, and the offer
 * is what it carries until the first one joins.
 *
 * The suggestions matter more here than they do for a customer. The platform matches
 * item names exactly, so a seller who types "Espresso Machine Pro" while buyers have
 * pooled behind "Espresso Machine" opens a second request and bids against nobody -
 * which is the split the whole arrangement exists to avoid. So a near match is put in
 * front of them, with the request it names to go and offer against instead.
 */
function OfferOnANewItemDialog() {
  const navigate = useNavigate()
  const dispatch = useAppDispatch()
  const similar = useAppSelector((s) => s.requests.similar)
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [form, setForm] = useState({
    itemName: '',
    category: '',
    itemDescription: '',
    availableQuantity: 1,
    pricePerUnit: '0.00',
    currency: 'EUR',
    description: '',
  })

  // An existing request for this exact item is not a suggestion but an answer: the offer
  // would land on it anyway, and the request page is where its demand can be read first.
  const exactMatch = similar.find((request) => request.exact)

  const set = (key: keyof typeof form) => (event: { target: { value: string } }) => {
    setFormError(null)
    setForm((f) => ({
      ...f,
      [key]: key === 'availableQuantity' ? Number(event.target.value) : event.target.value,
    }))
  }

  // The same lookup, debounce and floor the new-request form uses: below three
  // characters everything looks like everything.
  useEffect(() => {
    const itemName = form.itemName.trim()
    if (!open || itemName.length < 3) {
      dispatch(similarCleared())
      return
    }
    const id = setTimeout(() => void dispatch(fetchSimilarRequests(itemName)), 250)
    return () => clearTimeout(id)
  }, [dispatch, form.itemName, open])

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    setFormError(null)
    const result = await dispatch(
      createOffer({
        item: {
          itemName: form.itemName,
          description: form.itemDescription,
          category: form.category,
        },
        availableQuantity: form.availableQuantity,
        pricePerUnit: form.pricePerUnit,
        currency: form.currency,
        description: form.description,
      }),
    )
    setSaving(false)

    if (!createOffer.fulfilled.match(result)) {
      // A seller answers a request once. If this item turned out to have a request they
      // have already offered on, there is nothing to submit here - only the offer they
      // already made, on the page that can change it.
      const existing = result.payload?.existing
      if (existing) {
        dispatch(similarCleared())
        setOpen(false)
        navigate(`/requests/${existing.requestId}`)
        toast.info('You have already offered on this item', {
          description: 'Your offer is below — change its terms there rather than making a second.',
        })
        return
      }
      setFormError(result.payload?.message ?? 'Could not submit offer')
      return
    }

    dispatch(similarCleared())
    setOpen(false)
    // Onto the request the offer landed on - which may be one that already existed, since
    // the service will not open a second one for an item that already has demand. Showing
    // it is how the seller finds out which of the two happened.
    navigate(`/requests/${result.payload.requestId}`)
    toast.success('Offer submitted', {
      description: `An administrator reviews it next. Buyers can now join the request for ${form.itemName}.`,
    })
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) dispatch(similarCleared())
      }}
    >
      <DialogTrigger asChild>
        <Button size="lg">
          <HandshakeIcon />
          Offer on a new item
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Offer on a new item</DialogTitle>
          <DialogDescription>
            For stock nobody has asked for yet. The request opens with no buyers on it and your
            offer is what it carries — buyers join it afterwards, and an administrator reviews
            the offer as usual.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={submit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="offerItemName">Item</FieldLabel>
              <Input
                id="offerItemName"
                value={form.itemName}
                onChange={set('itemName')}
                placeholder="Espresso Machine"
                required
              />
              <FieldDescription>
                The name is what pools the demand. If buyers are already asking for this item,
                your offer joins their request rather than opening a second one.
              </FieldDescription>
            </Field>

            {/* Demand that already exists for what is being typed. Only exact names pool
                automatically, so a close one is the seller's own call - and it is worth
                making: an item buyers have already gathered behind is a better thing to
                bid on than one waiting for its first. */}
            {similar.length > 0 && (
              <div className="space-y-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3">
                <p className="flex items-center gap-2 text-sm font-medium">
                  <SearchIcon className="size-4" />
                  {exactMatch
                    ? 'This item already has a request'
                    : `${similar.length === 1 ? 'One request looks' : `${similar.length} requests look`} like this`}
                </p>
                <p className="text-xs text-muted-foreground">
                  {exactMatch
                    ? 'Your offer would land on it. Open it to read the demand first.'
                    : 'A name that is merely close opens a separate request, and you would be bidding against nobody. If one of these is the same product, offer against it instead.'}
                </p>
                <ul className="space-y-1.5">
                  {similar.map((request) => (
                    <li
                      key={request.requestId}
                      className="flex items-center gap-3 rounded-md bg-background p-2"
                    >
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">{request.itemName}</p>
                        <p className="text-xs text-muted-foreground tabular-nums">
                          {request.totalCustomers} buyers · {request.totalQuantity} units
                        </p>
                      </div>
                      <Button
                        type="button"
                        size="sm"
                        onClick={() => {
                          setOpen(false)
                          dispatch(similarCleared())
                          navigate(`/requests/${request.requestId!}`)
                        }}
                      >
                        Offer on this
                      </Button>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <div className="grid gap-5 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="offerItemCategory">Category</FieldLabel>
                <Input
                  id="offerItemCategory"
                  value={form.category}
                  onChange={set('category')}
                  placeholder="Kitchen"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="offerItemQuantity">Units you can supply</FieldLabel>
                <Input
                  id="offerItemQuantity"
                  type="number"
                  min={1}
                  value={form.availableQuantity}
                  onChange={set('availableQuantity')}
                  required
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="offerItemPrice">Price per unit</FieldLabel>
                <Input
                  id="offerItemPrice"
                  value={form.pricePerUnit}
                  onChange={set('pricePerUnit')}
                  required
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="offerItemCurrency">Currency</FieldLabel>
                <Input
                  id="offerItemCurrency"
                  value={form.currency}
                  onChange={set('currency')}
                  maxLength={3}
                  required
                />
              </Field>
            </div>

            <Field>
              <FieldLabel htmlFor="offerItemDescription">What the item is</FieldLabel>
              <Textarea
                id="offerItemDescription"
                value={form.itemDescription}
                onChange={set('itemDescription')}
                rows={2}
              />
              <FieldDescription>
                Shown on the request itself, so buyers can tell whether it is what they want.
              </FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="offerItemNotes">Notes on your offer</FieldLabel>
              <Textarea
                id="offerItemNotes"
                value={form.description}
                onChange={set('description')}
                rows={2}
              />
              <FieldDescription>
                What the buyers get for that price — lead time, condition, warranty.
              </FieldDescription>
            </Field>

            <ErrorAlert title="Could not submit this offer">{formError}</ErrorAlert>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={saving} variant={exactMatch ? 'outline' : 'default'}>
                {saving && <Spinner />}
                Submit offer
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function NewRequestDialog() {
  const navigate = useNavigate()
  const dispatch = useAppDispatch()
  const similar = useAppSelector((s) => s.requests.similar)
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [form, setForm] = useState({ itemName: '', quantity: 1 })

  // An open request already carrying this exact item is not a suggestion, it is an
  // answer: there is no second request to make, only demand to join.
  const exactMatch = similar.find((request) => request.exact)

  const set = (key: keyof typeof form) => (event: { target: { value: string } }) => {
    // A refusal belonged to the name it was typed against, so editing clears it.
    setFormError(null)
    setForm((f) => ({
      ...f,
      [key]: key === 'quantity' ? Number(event.target.value) : event.target.value,
    }))
  }

  // Ask what the name looks like as it is typed, so a customer meets the request they
  // meant before they have finished describing it themselves. Debounced on the same
  // 250ms the browse filters use; below three characters everything looks like
  // everything, so there is nothing worth showing yet.
  useEffect(() => {
    const itemName = form.itemName.trim()
    if (!open || itemName.length < 3) {
      dispatch(similarCleared())
      return
    }
    const id = setTimeout(() => void dispatch(fetchSimilarRequests(itemName)), 250)
    return () => clearTimeout(id)
  }, [dispatch, form.itemName, open])

  function done(title: string, description: string) {
    dispatch(fetchRequests())
    dispatch(similarCleared())
    setOpen(false)
    setFormError(null)
    setForm({ itemName: '', quantity: 1 })
    toast.success(title, { description })
  }

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    setFormError(null)
    const result = await dispatch(createRequest(form))
    setSaving(false)
    if (createRequest.fulfilled.match(result)) {
      done('Request created', 'Other buyers can now join it.')
      return
    }
    // A refusal arrives with the requests it matched, which the panel above is already
    // rendering by now - so this only has to say why the button did not work.
    setFormError(result.payload?.message ?? 'Could not create request')
  }

  async function join(request: PurchaseRequest) {
    setSaving(true)
    const result = await dispatch(
      // Generated from the OpenAPI document, where the id is not marked required, the
      // same way notificationId, accessId and offerId are asserted at their call sites.
      joinRequest({ id: request.requestId!, quantity: form.quantity }),
    )
    setSaving(false)
    if (joinRequest.fulfilled.match(result)) {
      // Joining ends on My requests: the quantity just added is the caller's own, and
      // this list shows everyone's. done() is deliberately not used - its refetch of the
      // browse list would be thrown away by the navigation on the next line.
      dispatch(similarCleared())
      setOpen(false)
      setFormError(null)
      setForm({ itemName: '', quantity: 1 })
      toast.success('You joined an existing request', {
        description: `Your ${form.quantity} joined the demand for ${request.itemName}.`,
      })
      navigate('/my-requests')
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        setOpen(next)
        if (!next) dispatch(similarCleared())
      }}
    >
      <DialogTrigger asChild>
        <Button size="lg">
          <PlusIcon />
          New request
        </Button>
      </DialogTrigger>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>New request</DialogTitle>
          <DialogDescription>
            Name the item and say how many you want. If someone has already asked for it, you
            join them - sellers offer against the combined demand, not your quantity alone.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={submit}>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="itemName">Item</FieldLabel>
              <Input
                id="itemName"
                value={form.itemName}
                onChange={set('itemName')}
                placeholder="Espresso Machine"
                required
              />
              <FieldDescription>
                The name is what pools the demand: an item already being asked for lands you in
                that request.
              </FieldDescription>
            </Field>

            {/* Demand that already exists for what is being typed. Never joined on the
                customer's behalf - being enrolled in a stranger's request because a name
                collided is not what anybody asked for - so it is put in front of them
                with a button, and the decision stays theirs. */}
            {similar.length > 0 && (
              <div className="space-y-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3">
                <p className="flex items-center gap-2 text-sm font-medium">
                  <SearchIcon className="size-4" />
                  {exactMatch
                    ? 'A request for this item already exists'
                    : `${similar.length === 1 ? 'One request looks' : `${similar.length} requests look`} like this`}
                </p>
                <p className="text-xs text-muted-foreground">
                  {exactMatch
                    ? 'You cannot open a second request for the same item. Join this one and your quantity is added to its demand.'
                    : 'If one of these is what you meant, join it - your demand pools with theirs and sellers bid against the combined total. Otherwise carry on and open your own.'}
                </p>
                <ul className="space-y-1.5">
                  {similar.map((request) => (
                    <li
                      key={request.requestId}
                      className="flex items-center gap-3 rounded-md bg-background p-2"
                    >
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-medium">{request.itemName}</p>
                        <p className="text-xs text-muted-foreground tabular-nums">
                          {request.totalCustomers === 0
                            ? 'No buyers yet — a seller is offering on it'
                            : `${request.totalCustomers} buyers · ${request.totalQuantity} units`}
                        </p>
                      </div>
                      <Button type="button" size="sm" disabled={saving} onClick={() => join(request)}>
                        Join with {form.quantity}
                      </Button>
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <Field>
              <FieldLabel htmlFor="quantity">How many you want</FieldLabel>
              <Input
                id="quantity"
                type="number"
                min={1}
                value={form.quantity}
                onChange={set('quantity')}
                required
                // Field lays its children out full-width; this one is three digits wide.
                className="w-32!"
              />
              <FieldDescription>
                Yours alone — joining buyers add theirs on top.
              </FieldDescription>
            </Field>

            <ErrorAlert title="Could not create this request">{formError}</ErrorAlert>

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              {/* Withheld entirely when the item is already open demand: there is no
                  create to make, and a button that can only fail is worse than none. */}
              {!exactMatch && (
                <Button
                  type="submit"
                  disabled={saving}
                  variant={similar.length > 0 ? 'outline' : 'default'}
                >
                  {saving && <Spinner />}
                  {similar.length > 0 ? 'Create a separate request' : 'Create request'}
                </Button>
              )}
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  )
}
