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

**Nothing is published inline.** Every producer writes the event to its own
`notification_outbox` table inside the transaction that caused it, and a relay drains
that table to the broker. The event and the business change are one write: there is no
window in which a request is joined but the notification has vanished, because a
failure rolls back both.

That is what makes a broker outage cost latency rather than the notification. The row
is already durable when the producer answers its caller, and the relay retries until it
is sent - so an event produced while RabbitMQ is down is delivered when it comes back,
with nobody restarting anything.

```
  business tx ──┬── the change (request, offer, decision)
                └── notification_outbox row        ← one commit

  relay loop  ──── SELECT … WHERE published_at IS NULL
                   FOR UPDATE SKIP LOCKED          ← safe with several replicas
                   publish → mark published_at
```

The `eventId` is fixed when the row is written, not when it is published. A relay that
dies between publishing and the commit that marks the row sent will publish it again on
restart with the same id, which the consumer recognises as a redelivery - so
at-least-once relaying stays exactly-once in effect.

The relay polls rather than using `LISTEN`/`NOTIFY`: a subscription would be lower
latency but would silently miss rows written while its connection was down, which is
the one case the outbox exists for. A poll re-reads the table and cannot miss anything.
Producers nudge it on commit, so the interval only matters when a nudge is lost.

**What is still not transactional:** admin-service's `Decide` relays the offer status to
offer-service over HTTP, and moving the request to `OFFER_APPROVED` is a second call
after the commit. Those are commands to other services rather than notifications, so
the outbox does not cover them; a command outbox would, at the cost of making an
approval asynchronous. See the README.

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
| `request.closed` | `REQUEST_CLOSED` | request-service | every participant |

`REQUEST_CLOSED` is the one genuine fan-out: one event carrying a notification per
participant, written in a single transaction with the status change. A close that
reached some participants and not others would be worse than one that reached none,
because the owner could not safely retry it.

Its recipients are resolved before the transaction opens - participation is recorded as
`customerId`s and notification-service is addressed by `userId`, so the producer makes
the hop through customer-service. Resolution is a network call per participant, and
holding a row lock across those is how a slow dependency becomes a database outage. A
participant whose lookup fails is skipped and logged rather than blocking the close.

## Why the HTTP internal API is still there

`POST /internal/notifications` and `/internal/notifications/bulk` are named in spec
section 13 and remain the synchronous path. They share the same code as the consumer,
so nothing can drift. Events are the production path; HTTP stays for spec conformance,
for a caller that genuinely needs to know the notification landed, and for testing
without a broker.
