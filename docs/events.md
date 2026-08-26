# Notification Events over AMQP

The contract every producer and the single consumer implement. It exists because the
notification steps of spec flows 1-3 must not be able to fail the business operation
they follow: a customer who joined a request has joined it, whether or not the "you
joined" message was delivered.

## Topology

```
                 exchange: marketplace.events  (topic, durable)
                                 |
        request.joined  ---------+
        offer.created   ---------+---->  queue: notification.events  (durable, binds #)
        offer.approved  ---------+                     |
        offer.rejected  ---------+              notification-service
        contact.access.granted --+                     |
                                                 on permanent failure
                                                       v
                          exchange: marketplace.events.dlx --> notification.events.dlq
```

`notification.events` binds `#` rather than a list of keys. Spec section 13 introduces
its event list with "Examples", so the set is open by design - the same reason the
`type` column is shape-checked instead of enumerated. A new event reaches the consumer
without a binding change.

The topology is declared by the services that use it, not by an operator or a compose
file, so a fresh broker needs no setup. Declarations are idempotent and identical on
every side, so it does not matter which service starts first.

## Envelope

Published as `application/json`, persistent delivery mode.

```json
{
  "eventId": "1f0c...-uuid",
  "occurredAt": "2026-08-26T09:41:12.482Z",
  "source": "request-service",
  "notifications": [
    {
      "userId": "8935afa2-...",
      "type": "REQUEST_JOINED",
      "channel": "IN_APP",
      "title": "Someone joined your request",
      "message": "A customer joined the request for an Espresso Machine."
    }
  ]
}
```

`notifications` carries the same fields as `POST /internal/notifications`, so both
paths funnel into one function in notification-service and cannot drift apart.

**The producer resolves the recipients.** It sends `userId`s and rendered text, never a
customerId, a sellerId or a role. That is what keeps notification-service free of
outbound dependencies - it never has to ask another service who anyone is.

**One event may carry several notifications**, and they are written in one transaction.
A fan-out that reached some recipients and not others, with no record of which, would
be worse than one that reached none, because the producer could not safely retry it.

## Delivery guarantees

**At-least-once, deduplicated on arrival.** AMQP redelivers after a consumer crash or a
missed ack, so `eventId` is inserted into `processed_event` in the same transaction as
the notifications. A redelivery hits the primary key, is recognised as already handled,
and is acked without writing anything twice.

**Publishing is best-effort and happens after the business transaction commits.** A
broker that is down cannot roll back a request that was genuinely joined; the failure
is logged and the notification is lost. Closing that gap needs a transactional outbox
in each producer - the same machinery that would close the dual-write window in
admin-service's `Decide` - and is deliberately not built here.

**A message that cannot be processed is dead-lettered, never requeued in place.** A
poison message requeued to the same queue is redelivered immediately and forever, which
takes the consumer down with it. `notification.events.dlq` holds them for inspection
and replay.

## Events

| Routing key | Type | Produced by | Recipient |
|---|---|---|---|
| `request.joined` | `REQUEST_JOINED` | request-service | the customer who joined |
| `offer.created` | `NEW_OFFER` | offer-service | every ADMIN |
| `offer.approved` | `OFFER_APPROVED` | admin-service | the seller |
| `offer.rejected` | `OFFER_REJECTED` | admin-service | the seller |
| `contact.access.granted` | `CONTACT_ACCESS_GRANTED` | admin-service | the seller |
| `request.closed` | `REQUEST_CLOSED` | *(no producer yet)* | every participant |

`REQUEST_CLOSED` is specified in section 18 but has no trigger: nothing in the platform
moves a request out of `OPEN` today. The consumer handles it already - it needs a
close operation in request-service, which is a business change rather than wiring.

## Why the HTTP internal API is still there

`POST /internal/notifications` and `/internal/notifications/bulk` are named in spec
section 13 and remain the synchronous path. They share the same code as the consumer,
so nothing can drift. Events are the production path; HTTP stays for spec conformance,
for a caller that genuinely needs to know the notification landed, and for testing
without a broker.
