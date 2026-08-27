/** Open demand. Any authenticated role may browse - that is how a seller finds work. */

import { useEffect, useState } from 'react'
import { LogInIcon, PackageIcon, PlusIcon, SearchIcon, UsersIcon } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Skeleton } from '@/components/ui/skeleton'
import { Spinner } from '@/components/ui/spinner'
import { Textarea } from '@/components/ui/textarea'
import type { PurchaseRequest, RequestStatus } from '@/api/types'
import { useAppDispatch, useAppSelector } from '@/store'
import { createRequest, fetchRequests } from '@/store/requestsSlice'

/** Any status at all - Radix's Select has no value for "none selected". */
const ANY = 'ANY'

export default function BrowseRequests() {
  const dispatch = useAppDispatch()
  const { browse, loading, error } = useAppSelector((s) => s.requests)
  const role = useAppSelector((s) => s.auth.user?.role)
  // The token, not the role: mid session-restore there is a token but no role yet, and
  // flashing "sign in" at somebody who already is would be worse than showing nothing.
  const signedIn = useAppSelector((s) => Boolean(s.auth.accessToken))
  const location = useLocation()
  const [filters, setFilters] = useState<{ q: string; status: RequestStatus | typeof ANY }>({
    q: '',
    status: 'OPEN',
  })

  useEffect(() => {
    // Typing should not fire a request per keystroke; the pause is short enough that
    // the list still feels live.
    const id = setTimeout(() => {
      dispatch(
        fetchRequests({
          q: filters.q || undefined,
          status: filters.status === ANY ? undefined : filters.status,
        }),
      )
    }, 250)
    return () => clearTimeout(id)
  }, [dispatch, filters])

  return (
    <div className="space-y-6">
      <PageHeader
        title="Open demand"
        description="Every request buyers have pooled. Sellers bid against the combined total, not one buyer's quantity."
      >
        {role === 'CUSTOMER' && <NewRequestDialog />}

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
        <CardContent className="flex flex-wrap items-center gap-3">
          <div className="relative min-w-48 flex-1">
            <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={filters.q}
              placeholder="Search for an espresso machine…"
              aria-label="Search requests"
              className="pl-8"
              onChange={(e) => setFilters((f) => ({ ...f, q: e.target.value }))}
            />
          </div>

          <Select
            value={filters.status}
            onValueChange={(status) =>
              setFilters((f) => ({ ...f, status: status as RequestStatus | typeof ANY }))
            }
          >
            <SelectTrigger className="w-52" aria-label="Filter by status">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ANY}>Any status</SelectItem>
              <SelectItem value="OPEN">Open</SelectItem>
              <SelectItem value="OFFER_APPROVED">Offer approved</SelectItem>
              <SelectItem value="CLOSED">Closed</SelectItem>
            </SelectContent>
          </Select>

          <Separator orientation="vertical" className="hidden h-6! sm:block" />

          <p className="text-sm text-muted-foreground tabular-nums">
            {loading ? <Spinner className="size-4" /> : `${browse.length} shown`}
          </p>
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
            <EmptyTitle>Nothing matches those filters</EmptyTitle>
            <EmptyDescription>
              Try a different search, or widen the status filter to any.
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
        </CardContent>
      </Card>
    </Link>
  )
}

function NewRequestDialog() {
  const dispatch = useAppDispatch()
  const [open, setOpen] = useState(false)
  const [saving, setSaving] = useState(false)
  const [form, setForm] = useState({ itemName: '', description: '', category: '', quantity: 1 })

  const set = (key: keyof typeof form) => (event: { target: { value: string } }) =>
    setForm((f) => ({
      ...f,
      [key]: key === 'quantity' ? Number(event.target.value) : event.target.value,
    }))

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    setSaving(true)
    const result = await dispatch(createRequest(form))
    setSaving(false)
    if (createRequest.fulfilled.match(result)) {
      dispatch(fetchRequests())
      setOpen(false)
      setForm({ itemName: '', description: '', category: '', quantity: 1 })
      toast.success('Request created', { description: 'Other buyers can now join it.' })
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
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
            Other buyers can join yours. Sellers offer against the combined demand, not your
            quantity alone.
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={submit}>
          <FieldGroup>
            <div className="grid gap-5 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="itemName">Item</FieldLabel>
                <Input id="itemName" value={form.itemName} onChange={set('itemName')} required />
              </Field>
              <Field>
                <FieldLabel htmlFor="category">Category</FieldLabel>
                <Input
                  id="category"
                  value={form.category}
                  onChange={set('category')}
                  placeholder="kitchen"
                  required
                />
              </Field>
            </div>

            <Field>
              <FieldLabel htmlFor="description">Description</FieldLabel>
              <Textarea
                id="description"
                value={form.description}
                onChange={set('description')}
                rows={3}
                required
              />
            </Field>

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

            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={saving}>
                {saving && <Spinner />}
                Create request
              </Button>
            </DialogFooter>
          </FieldGroup>
        </form>
      </DialogContent>
    </Dialog>
  )
}
