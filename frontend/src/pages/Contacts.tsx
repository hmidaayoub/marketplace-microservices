/**
 * The seller's payoff, and the only place in the platform a phone number is shown.
 *
 * There is no seller-facing endpoint that lists their grants - /api/admin/contact-access
 * is ADMIN-only - but an approved offer *is* the grant: approving one is exactly what
 * writes the contact_access rows for that seller and that request. So the seller's own
 * offers are the index, and nobody has to know a request id to use this page.
 *
 * A 403 on lookup is still possible and still normal: access can be revoked or expire
 * after the offer was approved, and the check runs on every call rather than at grant
 * time.
 */

import { useEffect } from 'react'
import { Link } from 'react-router-dom'

import { useAppDispatch, useAppSelector } from '../store'
import { contactsCleared, fetchContacts } from '../store/adminSlice'
import { fetchMyOffers } from '../store/offersSlice'
import { Alert, Button, Card, Empty } from '../components/ui'

export default function Contacts() {
  const dispatch = useAppDispatch()
  const { contacts, error } = useAppSelector((s) => s.admin)
  const offers = useAppSelector((s) => s.offers.mine)

  useEffect(() => {
    dispatch(fetchMyOffers())
    return () => {
      dispatch(contactsCleared())
    }
  }, [dispatch])

  const approved = offers.filter((offer) => offer.status === 'APPROVED')

  return (
    <div className="space-y-4">
      <h1 className="text-2xl font-semibold">Customer contacts</h1>
      <p className="text-sm text-slate-600">
        Released to you when an administrator approves one of your offers, for the buyers on that
        request only. Every lookup is checked and recorded, and access can be revoked.
      </p>

      {approved.length === 0 && (
        <Empty>
          No approved offers yet — contacts unlock when an administrator approves one.{' '}
          <Link to="/my-offers" className="font-medium text-brand-600 hover:underline">
            See your offers
          </Link>
        </Empty>
      )}

      {approved.map((offer) => {
        const open = contacts?.requestId === offer.requestId
        return (
          <Card key={offer.offerId} className="space-y-3">
            <div className="flex items-center justify-between gap-4">
              <div>
                <p className="font-medium">
                  {offer.availableQuantity} units · {offer.pricePerUnit} {offer.currency}
                </p>
                <Link
                  to={`/requests/${offer.requestId}`}
                  className="text-sm text-brand-600 hover:underline"
                >
                  View the request
                </Link>
              </div>
              <Button
                variant={open ? 'ghost' : 'primary'}
                onClick={() =>
                  open
                    ? dispatch(contactsCleared())
                    : dispatch(fetchContacts(offer.requestId as string))
                }
              >
                {open ? 'Hide' : 'Show contacts'}
              </Button>
            </div>

            {open && (
              <ul className="divide-y divide-slate-200 border-t border-slate-200 pt-2">
                {contacts?.contacts?.length ? (
                  contacts.contacts.map((contact) => {
                    const name = [contact.firstName, contact.lastName].filter(Boolean).join(' ')
                    return (
                      <li
                        key={contact.customerId}
                        className="flex items-center justify-between gap-4 py-2 text-sm"
                      >
                        {/* The name is who to ask for; the id is only useful for support,
                            so it is present but subordinate. */}
                        <span className="font-medium text-slate-800">
                          {name || 'Unnamed customer'}
                        </span>
                        <a
                          href={`tel:${contact.phoneNumber}`}
                          className="font-medium text-brand-600 hover:underline"
                        >
                          {contact.phoneNumber}
                        </a>
                      </li>
                    )
                  })
                ) : (
                  <li className="py-2 text-sm text-slate-500">No buyers on this request.</li>
                )}
              </ul>
            )}
          </Card>
        )
      })}

      <Alert tone={error?.includes('granted') ? 'info' : 'error'}>{error}</Alert>
    </div>
  )
}
