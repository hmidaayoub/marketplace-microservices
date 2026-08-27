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
import { ContactIcon, EyeOffIcon, PhoneIcon, UserRoundIcon } from 'lucide-react'
import { Link } from 'react-router-dom'

import { ErrorAlert } from '@/components/error-alert'
import { PageHeader } from '@/components/page-header'
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
import { Separator } from '@/components/ui/separator'
import { useAppDispatch, useAppSelector } from '@/store'
import { contactsCleared, fetchContacts } from '@/store/adminSlice'
import { fetchMyOffers } from '@/store/offersSlice'

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
    <div className="space-y-6">
      <PageHeader
        title="Customer contacts"
        description="Released to you when an administrator approves one of your offers, for the buyers on that request only. Every lookup is checked and recorded, and access can be revoked."
      />

      {approved.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <ContactIcon />
            </EmptyMedia>
            <EmptyTitle>No approved offers yet</EmptyTitle>
            <EmptyDescription>
              Contacts unlock when an administrator approves one of your offers — nothing else in
              the platform releases a phone number.
            </EmptyDescription>
          </EmptyHeader>
          <EmptyContent>
            <Button variant="outline" asChild>
              <Link to="/my-offers">See your offers</Link>
            </Button>
          </EmptyContent>
        </Empty>
      ) : (
        <div className="space-y-3">
          {approved.map((offer) => {
            const open = contacts?.requestId === offer.requestId
            return (
              <Card key={offer.offerId}>
                <CardContent className="space-y-4">
                  <div className="flex flex-wrap items-center justify-between gap-4">
                    <div className="space-y-0.5">
                      <p className="font-medium tabular-nums">
                        {offer.availableQuantity} units · {offer.pricePerUnit} {offer.currency}
                      </p>
                      <Link
                        to={`/requests/${offer.requestId}`}
                        className="text-sm font-medium text-primary hover:underline"
                      >
                        View the request
                      </Link>
                    </div>

                    <Button
                      variant={open ? 'outline' : 'default'}
                      onClick={() =>
                        open
                          ? dispatch(contactsCleared())
                          : dispatch(fetchContacts(offer.requestId as string))
                      }
                    >
                      {open ? <EyeOffIcon /> : <PhoneIcon />}
                      {open ? 'Hide' : 'Show contacts'}
                    </Button>
                  </div>

                  {open && (
                    <>
                      <Separator />
                      <ul className="divide-y">
                        {contacts?.contacts?.length ? (
                          contacts.contacts.map((contact) => {
                            const name = [contact.firstName, contact.lastName]
                              .filter(Boolean)
                              .join(' ')
                            return (
                              <li
                                key={contact.customerId}
                                className="flex items-center justify-between gap-4 py-2.5 text-sm"
                              >
                                {/* The name is who to ask for; the number is what you do
                                    with it. A row that is only a number was the original
                                    defect, so the name leads. */}
                                <span className="flex items-center gap-2 font-medium">
                                  <UserRoundIcon className="size-4 text-muted-foreground" />
                                  {name || 'Unnamed customer'}
                                </span>
                                <a
                                  href={`tel:${contact.phoneNumber}`}
                                  className="font-medium text-primary tabular-nums hover:underline"
                                >
                                  {contact.phoneNumber}
                                </a>
                              </li>
                            )
                          })
                        ) : (
                          <li className="py-2.5 text-sm text-muted-foreground">
                            No buyers on this request.
                          </li>
                        )}
                      </ul>
                    </>
                  )}
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      <ErrorAlert
        tone={error?.includes('granted') ? 'info' : 'error'}
        title={error?.includes('granted') ? 'Not released yet' : undefined}
      >
        {error}
      </ErrorAlert>
    </div>
  )
}
