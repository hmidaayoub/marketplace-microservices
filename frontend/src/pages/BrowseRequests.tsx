/** Open demand. Any authenticated role may browse - that is how a seller finds work. */

import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

import type { RequestStatus } from '../api/types'
import { useAppDispatch, useAppSelector } from '../store'
import { createRequest, fetchRequests } from '../store/requestsSlice'
import { Alert, Badge, Button, Card, Empty, Field, Input, Select } from '../components/ui'

export default function BrowseRequests() {
  const dispatch = useAppDispatch()
  const { browse, loading, error } = useAppSelector((s) => s.requests)
  const role = useAppSelector((s) => s.auth.user?.role)
  const [filters, setFilters] = useState<{ q: string; status: RequestStatus | '' }>({
    q: '',
    status: 'OPEN',
  })
  const [composing, setComposing] = useState(false)

  useEffect(() => {
    dispatch(fetchRequests({ q: filters.q || undefined, status: filters.status || undefined }))
  }, [dispatch, filters])

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Open demand</h1>
        {role === 'CUSTOMER' && (
          <Button onClick={() => setComposing((c) => !c)}>
            {composing ? 'Cancel' : 'New request'}
          </Button>
        )}
      </div>

      {composing && <NewRequestForm onDone={() => setComposing(false)} />}

      <Card className="flex flex-wrap items-end gap-3">
        <div className="min-w-48 flex-1">
          <Field label="Search">
            <Input
              value={filters.q}
              placeholder="Espresso machine…"
              onChange={(e) => setFilters((f) => ({ ...f, q: e.target.value }))}
            />
          </Field>
        </div>
        <div className="w-48">
          <Field label="Status">
            <Select
              value={filters.status}
              onChange={(e) =>
                setFilters((f) => ({ ...f, status: e.target.value as RequestStatus | '' }))
              }
            >
              <option value="">Any</option>
              <option value="OPEN">Open</option>
              <option value="OFFER_APPROVED">Offer approved</option>
              <option value="CLOSED">Closed</option>
            </Select>
          </Field>
        </div>
      </Card>

      <Alert>{error}</Alert>

      {loading && <Empty>Loading…</Empty>}
      {!loading && browse.length === 0 && <Empty>Nothing matches those filters yet.</Empty>}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        {browse.map((request) => (
          <Link key={request.requestId} to={`/requests/${request.requestId}`}>
            <Card className="h-full transition hover:border-brand-500 hover:shadow">
              <div className="mb-2 flex items-start justify-between gap-2">
                <h2 className="font-medium text-slate-900">{request.itemName}</h2>
                <Badge>{request.status ?? 'OPEN'}</Badge>
              </div>
              <p className="mb-3 line-clamp-2 text-sm text-slate-600">{request.description}</p>
              <dl className="flex gap-4 text-sm">
                <div>
                  <dt className="text-slate-500">Buyers</dt>
                  <dd className="font-semibold">{request.totalCustomers}</dd>
                </div>
                <div>
                  <dt className="text-slate-500">Units wanted</dt>
                  <dd className="font-semibold">{request.totalQuantity}</dd>
                </div>
              </dl>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  )
}

function NewRequestForm({ onDone }: { onDone: () => void }) {
  const dispatch = useAppDispatch()
  const [form, setForm] = useState({
    itemName: '',
    description: '',
    category: '',
    quantity: 1,
  })

  const set = (key: keyof typeof form) => (event: { target: { value: string } }) =>
    setForm((f) => ({
      ...f,
      [key]: key === 'quantity' ? Number(event.target.value) : event.target.value,
    }))

  async function submit(event: React.FormEvent) {
    event.preventDefault()
    const result = await dispatch(createRequest(form))
    if (createRequest.fulfilled.match(result)) {
      dispatch(fetchRequests())
      onDone()
    }
  }

  return (
    <Card>
      <form onSubmit={submit} className="grid gap-3 sm:grid-cols-2">
        <Field label="Item">
          <Input value={form.itemName} onChange={set('itemName')} required />
        </Field>
        <Field label="Category">
          <Input value={form.category} onChange={set('category')} placeholder="kitchen" required />
        </Field>
        <div className="sm:col-span-2">
          <Field label="Description">
            <Input value={form.description} onChange={set('description')} required />
          </Field>
        </div>
        <Field label="How many you want">
          <Input type="number" min={1} value={form.quantity} onChange={set('quantity')} required />
        </Field>
        <div className="flex items-end">
          <Button type="submit">Create request</Button>
        </div>
        <p className="text-xs text-slate-500 sm:col-span-2">
          Other buyers can join your request. Sellers offer against the combined demand, not your
          quantity alone.
        </p>
      </form>
    </Card>
  )
}
