/**
 * The seller's payoff, and the only place in the platform a phone number is shown.
 *
 * A 403 here is the normal state before an admin approves, not a fault, so it reads as
 * an explanation rather than an error.
 */

import { useState } from 'react'

import { useAppDispatch, useAppSelector } from '../store'
import { contactsCleared, fetchContacts } from '../store/adminSlice'
import { Alert, Button, Card, Empty, Field, Input } from '../components/ui'

export default function Contacts() {
  const dispatch = useAppDispatch()
  const { contacts, error } = useAppSelector((s) => s.admin)
  const [requestId, setRequestId] = useState('')

  function submit(event: React.FormEvent) {
    event.preventDefault()
    dispatch(contactsCleared())
    if (requestId) dispatch(fetchContacts(requestId))
  }

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Customer contacts</h1>
      <p className="text-sm text-slate-600">
        Available for a request once an administrator approves your offer on it. Access can be
        revoked, and every release is recorded.
      </p>

      <Card>
        <form onSubmit={submit} className="flex items-end gap-3">
          <div className="flex-1">
            <Field label="Request id">
              <Input
                value={requestId}
                onChange={(e) => setRequestId(e.target.value)}
                placeholder="Paste the id from My offers"
              />
            </Field>
          </div>
          <Button type="submit">Look up</Button>
        </form>
      </Card>

      <Alert tone={error?.includes('granted') ? 'info' : 'error'}>{error}</Alert>

      {contacts && (
        <Card>
          <h2 className="mb-3 font-medium">
            {contacts.contacts?.length ?? 0} contact(s) for this request
          </h2>
          <ul className="divide-y divide-slate-200">
            {contacts.contacts?.map((contact) => (
              <li key={contact.customerId} className="flex justify-between py-2 text-sm">
                <span className="text-slate-500">{contact.customerId}</span>
                <a
                  href={`tel:${contact.phoneNumber}`}
                  className="font-medium text-brand-600 hover:underline"
                >
                  {contact.phoneNumber}
                </a>
              </li>
            ))}
          </ul>
        </Card>
      )}

      {!contacts && !error && <Empty>Enter a request id to see the contacts released to you.</Empty>}
    </div>
  )
}
