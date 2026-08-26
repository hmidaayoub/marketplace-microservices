# Marketplace Microservices

A microservices-based marketplace platform where customers group together into purchase
requests, sellers submit offers against the aggregated demand, and an administrator
approves an offer before the seller is granted access to customer contact details.

The defining constraint of the design is that **phone numbers never leave the Auth
Service** except through an explicitly granted, audited permission. No request, offer, or
seller service ever stores or returns one.

## Status

| Service | Port | Database | State |
|---|---|---|---|
| Auth | 8081 | `user_auth_db` | Implemented |
| Customer | 8082 | `customer_db` | Implemented |
| Seller | 8083 | `seller_db` | Implemented |
| Request | 8084 | `request_db` | Implemented (Go) |
| Offer | 8085 | `offer_db` | Implemented (Python) |
| Admin/Contact | 8086 | `admin_contact_db` | Implemented (Go) |
| Notification | 8087 | `notification_db` | Implemented (Python) |
| API Gateway | 8080 | — | Implemented (nginx) |
| Swagger UI | 8080`/docs` | — | Aggregates all seven specs |

315 tests pass in total: 87 across the four Maven modules, 96 across the two Go
modules (60 request, 36 admin), and 132 across the two Python modules (66 offer,
66 notification).

Every service in the spec is implemented, the notification events of section 18 flow
between them over RabbitMQ, and the nine public routes of section 19 are served by the
gateway. **8080 is the only port the platform publishes.** The seven services are
reachable only from inside the compose network, which is what makes section 6's rule -
`/internal` must not be exposed publicly - a property of the topology rather than a
convention every service has to remember.

The platform is deliberately polyglot: Auth, Customer and Seller are Spring Boot,
Request and Admin/Contact are Go, Offer and Notification are Python. They interoperate
only through HTTP and a shared JWT secret, which is the boundary the architecture
already assumed - no service reads another's database, so the language behind an
endpoint is not something its callers can observe. All three stacks return the same
error shape, and a malformed path variable reads identically whichever one answers it.

## Tech Stack

| Concern | Choice | Notes |
|---|---|---|
| Language | Java 21 | |
| Framework | Spring Boot 3.2.5 | |
| **ORM** | **Hibernate 6.4.4 via Spring Data JPA** | `ddl-auto: validate` — Hibernate never creates or alters schema |
| Schema migrations | Flyway 9.22.3 | `src/main/resources/db/migration`, the single source of truth for schema |
| Database | PostgreSQL 15 | One database per service; no service reads another's |
| Authentication | JJWT 0.12.5 (HS256) | |
| Build | Maven, multi-module | Root aggregator `pom.xml` owns all shared versions |
| Testing | JUnit 5, Mockito, AssertJ | |
| Integration testing | Testcontainers (PostgreSQL 15) | Real Postgres, real Flyway migrations |
| Service stubbing | WireMock (spring-cloud-contract) | For cross-service HTTP in tests |
| Boilerplate | Lombok | |
| Observability | Spring Boot Actuator | `/actuator/health` for probes |
| Messaging | RabbitMQ 3.13 (AMQP) | Notification events; topic exchange — see `docs/events.md` |
| Edge | nginx 1.27 (alpine) | The nine spec routes, default-deny — `infrastructure/docker/gateway/nginx.conf` |
| Containers | Docker, Docker Compose | Multi-stage builds, non-root runtime user |

### Go stack (request-service, admin-service)

| Concern | Choice | Notes |
|---|---|---|
| Language | Go 1.25 | |
| Router | chi v5 | Middleware chain mirrors the Spring filter chain |
| **Data access** | **sqlc + pgx v5** | SQL is hand-written; sqlc generates typed Go from it |
| Schema migrations | golang-migrate | `internal/db/migrations`, embedded in the binary via `go:embed` |
| JWT | golang-jwt v5 | Verifies tokens minted by auth-service's jjwt |
| Integration testing | testcontainers-go (PostgreSQL 15) | Real Postgres, real migrations |

### Python stack (offer-service, notification-service)

| Concern | Choice | Notes |
|---|---|---|
| Language | Python 3.13 | Pinned via `.python-version`; managed by uv |
| Framework | FastAPI + uvicorn | |
| **ORM** | **SQLAlchemy 2.0 (async) + asyncpg** | Models never create schema, mirroring `ddl-auto: validate` |
| Schema migrations | Alembic | `migrations/versions`, applied at startup |
| Validation | Pydantic v2 | Wire schemas kept separate from ORM models, so responses can withhold fields |
| JWT | PyJWT | Verifies tokens minted by auth-service's jjwt |
| Packaging | uv | `uv.lock` pins the full dependency graph |
| Testing | pytest + testcontainers | Real Postgres, real migrations |
| Linting | ruff | |

Planned but not yet wired in: Kubernetes, Terraform, GitHub Actions,
Prometheus & Grafana.

### Notes on the ORM setup

- **Hibernate never owns the schema.** Every service runs `ddl-auto: validate`, so the
  application refuses to start if the entities and the Flyway-migrated schema disagree.
  Schema changes go in a new `V__` migration, never into an entity alone.
- **`open-in-view` is disabled** in all services, so lazy loading cannot silently run
  queries during response rendering.
- Integration tests run Flyway against a real PostgreSQL container rather than an
  in-memory database, so Postgres-specific SQL such as `gen_random_uuid()` is actually
  exercised.

## Module Layout

```
marketplace-microservices/
├── pom.xml              aggregator: modules + shared dependencyManagement
├── common-security/     shared JWT + internal API-key filters
├── auth-service/
├── customer-service/
├── seller-service/
├── request-service/    Go module, independent of the Maven reactor
├── admin-service/      Go module, independent of the Maven reactor
├── offer-service/      Python module, independent of the Maven reactor
└── notification-service/  Python module, independent of the Maven reactor
```

`common-security` holds `JwtUtil`, `JwtAuthenticationFilter` and `InternalApiKeyFilter`
under `com.marketplace.common.security`. Services pick these up with an explicit
`@ComponentScan`, since Spring's default scan only covers each application's own package.

## Security Model

There are three ways to call a service, and they are enforced differently.

**1. Public API — `/api/**`, end users.** Requires `Authorization: Bearer <jwt>`. Identity
is always taken from the token's `sub` claim, never from a request header or body.

```json
{ "sub": "<userId>", "role": "CUSTOMER | SELLER | ADMIN" }
```

**Signing algorithm is derived from the secret length.** jjwt's `signWith(SecretKey)`
picks the HMAC variant from the key size, so the 48-byte development secret produces
**HS384**, not HS256. Any verifier that pins a single algorithm will reject every real
token the moment the secret length changes. request-service therefore accepts the HMAC
family and rejects everything else, which still closes off `alg: none` and RS/HS
confusion. `request-service/internal/auth/jwt_test.go` asserts this against golden
tokens minted by the actual Java stack.

**2. Internal API — `/internal/**`, service-to-service.** Requires a shared secret in
`X-Internal-Api-Key`, compared in constant time. Callers attach it automatically via a
`RestTemplate` interceptor. These endpoints are not routable through the gateway: it
serves nine explicit `/api` prefixes and answers everything else with 404, so `/internal`
has no route rather than being excluded by a rule that has to be maintained. The gateway
also strips inbound `X-Internal-Api-Key` and `X-User-Id`, so neither can be smuggled in
from outside.

**4. Browser clients — CORS, answered at the gateway.** The origin allowlist lives in one
`map` in `nginx.conf` (`localhost:3000`, `:4200`, `:5173` for the usual dev servers; a
deployment adds its own). The origin is echoed rather than answered with `*`, and an
unlisted origin simply gets no `Access-Control-Allow-Origin` — the request is not served
differently, it is only unreadable by the page. Preflight `OPTIONS` is answered at the
edge with 204 rather than proxied, because a preflight never carries the `Authorization`
header and every service would reject it before the real request was sent. The headers
are set `always`, so a browser can still read the body of a deliberate 401 or 403 instead
of reporting an opaque CORS failure. `X-Internal-Api-Key` is deliberately not in
`Access-Control-Allow-Headers`: it is not a browser credential.
The filter **fails closed**: if no key is configured, every internal request is rejected
rather than served.

**3. Actuator.** `/actuator/health` and `/actuator/info` are open so probes can reach them;
every other actuator endpoint, metrics included, requires the internal key.

Passwords are hashed with BCrypt (strength 12).

### Response projections

Endpoints deliberately return different shapes to different audiences:

| Endpoint | Returns | Withholds |
|---|---|---|
| `GET /api/users/me` | Full user, phone included | — |
| `GET /internal/users/{id}` | `InternalUserResponse` | **phone number** |
| `GET /internal/users/{id}/phone` | Phone only | — |
| `GET /api/sellers/me` | Full seller | — |
| `GET /api/sellers/{id}` | `SellerPublicResponse` | **address, userId** |
| `GET /api/requests/{id}` | Request plus demand totals | **participant customerIds** |
| `GET /internal/requests/{id}/participants` | `customerIds` | — |
| `GET /api/offers/{id}` (rival seller) | `CompetingOfferOut` | **sellerId** |
| `GET /api/offers/{id}` (owner, customer, admin) | Full offer | — |
| `GET /api/contacts/requests/{id}` (granted seller) | `customerId` + **phone** | everything else |
| `GET /api/contacts/requests/{id}` (ungranted seller) | 403 | **the whole list** |
| `GET /internal/contact-access` | `allowed` boolean | the grant records behind it |

## Request Service

Owns demand: a purchase request is one item several customers want, and the participants
who want it. Endpoints follow spec section 10.

| Method | Endpoint | Who |
|---|---|---|
| `POST` | `/api/requests` | CUSTOMER |
| `GET` | `/api/requests` | any authenticated user |
| `GET` | `/api/requests/{requestId}` | any authenticated user |
| `GET` | `/api/requests/me` | CUSTOMER |
| `POST` | `/api/requests/{requestId}/participants` | CUSTOMER |
| `PUT` | `/api/requests/{requestId}/participants/me` | CUSTOMER |
| `DELETE` | `/api/requests/{requestId}/participants/me` | CUSTOMER |
| `POST` | `/api/requests/{requestId}/close` | CUSTOMER, the creator |
| `GET` | `/internal/requests/{requestId}` | internal key |
| `GET` | `/internal/requests/{requestId}/demand` | internal key |
| `GET` | `/internal/requests/{requestId}/participants` | internal key |
| `PATCH` | `/internal/requests/{requestId}/status` | internal key |

Browsing is open to any authenticated caller because that is how a seller finds demand
worth an offer. Everything that records participation is a CUSTOMER action.

Rules worth knowing:

- **Creating a request enrolls its creator** as the first participant (R1 + R3). A
  request never exists that nobody wants, so demand starts at one customer.
- **`totalCustomers` and `totalQuantity` are never written by a caller** (R4). Every
  participant change recomputes them from `request_participant` in the same transaction,
  behind a `SELECT ... FOR UPDATE` on the request row — without that lock, concurrent
  joins each recompute from a snapshot that misses the others and the stored total lands
  too low.
- **A customer joins a request at most once.** Enforced by a unique constraint rather
  than a read-then-write check, which two concurrent joins would both pass.
- **Participants may only change while the request is `OPEN`.** Once an offer is in
  flight, the demand it was priced against stops moving.
- **Identity is never accepted from the caller.** The token's `sub` is resolved to a
  `customerId` through customer-service's internal API; a body carrying `customerId` is
  rejected, not ignored.
- **No phone numbers, ever** (R10). `customerId`s leave only through
  `/internal/requests/{id}/participants`, which is what Admin/Contact uses to grant
  ContactAccess — so a seller browsing demand cannot enumerate the customers behind it.
- **A request has an owner, and only the owner closes it.** `created_by` is set when the
  request is created, because closing withdraws demand that other people joined — that
  is not a decision any participant should be able to make for the rest. A participant
  attempting it gets 403 rather than 404: they can already read the request, they simply
  did not create it. Closing emits `REQUEST_CLOSED` to every participant, one event
  carrying them all, written in the same transaction as the status change.
- **The status lifecycle is now actually driven.** `PATCH /internal/requests/{id}/status`
  is how Admin/Contact marks a request as having an approved offer. Offer-service has
  always refused new offers against an `OFFER_APPROVED` request; until something made
  that transition, the guard could never fire. The internal API accepts only forward
  transitions — Admin/Contact decides offers, not demand, so it cannot reopen a request.

## Offer Service

Owns seller proposals against aggregated demand (spec section 11).

| Method | Endpoint | Who |
|---|---|---|
| `POST` | `/api/offers` | SELLER |
| `GET` | `/api/offers/me` | SELLER |
| `GET` | `/api/offers/{offerId}` | any authenticated user |
| `GET` | `/api/offers/request/{requestId}` | any authenticated user |
| `PUT` | `/api/offers/{offerId}` | SELLER, owner, while PENDING |
| `DELETE` | `/api/offers/{offerId}` | SELLER, owner, while PENDING |
| `GET` | `/internal/offers/pending` | internal key |
| `GET` | `/internal/offers/{offerId}` | internal key |
| `PATCH` | `/internal/offers/{offerId}/status` | internal key |

Rules worth knowing:

- **An offer may only target a request that exists** (R5). Submission calls
  request-service's internal API first; an unknown request is a 404, and a request that
  is `OFFER_APPROVED`, `CLOSED` or `CANCELLED` is a 409. `OFFER_PENDING` still accepts
  offers — several sellers compete on the same demand until an admin picks one.
- **A new offer is always `PENDING`** (R6). `status` is not a field on the create schema
  at all, so a caller cannot start one anywhere else.
- **Only a `PENDING` offer can be changed or decided.** Once approved, contact
  permission may already have been granted against exactly the terms the admin saw, so
  letting the seller rewrite the price afterwards would change what was approved. A
  second decision on the same offer is a 409 rather than a silent overwrite.
- **Cancelling is a status change, not a delete.** The record survives for the audit
  history that Admin/Contact refers back to.
- **A seller cannot see who they are bidding against.** Customers and admins get the
  full offer; a rival seller gets `CompetingOfferOut`, which carries the price and
  quantity but withholds `sellerId`. Sellers still need the competitive field to price
  sensibly — they do not need the identity behind it.
- **R7 is enforced upstream.** Admin/Contact authenticates the ADMIN and writes the
  audit record, then relays the outcome through the internal `PATCH`, which accepts only
  `APPROVED` or `REJECTED`. This service holds status, not authority. The internal
  read-by-id exists for the same caller: the grant an approval produces links seller,
  request and offer together, so Admin/Contact must take those ids from the service that
  owns them rather than from the admin submitting the decision.
- **Identity is never accepted from the caller.** The token's `sub` is resolved to a
  `sellerId` through seller-service; a body carrying `sellerId` or `status` is rejected
  outright, not ignored.

## Admin/Contact Service

Decides offers and owns permission to expose customer contact information (spec
section 12). It is the most connected service in the platform: deciding an offer
touches offer-service and request-service, and answering a seller's contact lookup
walks seller-service, customer-service and auth-service in turn.

| Method | Endpoint | Who |
|---|---|---|
| `GET` | `/api/admin/offers/pending` | ADMIN |
| `POST` | `/api/admin/offers/{offerId}/approve` | ADMIN |
| `POST` | `/api/admin/offers/{offerId}/reject` | ADMIN |
| `GET` | `/api/admin/contact-access` | ADMIN |
| `DELETE` | `/api/admin/contact-access/{accessId}` | ADMIN |
| `GET` | `/api/contacts/requests/{requestId}` | SELLER |
| `GET` | `/internal/contact-access` | internal key |

Rules worth knowing:

- **This is where R7 lives.** `/api/admin/**` is ADMIN-only as a whole subtree, so a
  new admin route cannot be added without inheriting the check. Offer-service holds the
  resulting status but not the authority to set it — it accepts the outcome through its
  internal `PATCH` and refuses anything but `APPROVED` or `REJECTED`.
- **Approving does not expose a phone number** (R8). It writes one `ContactAccess` row
  per customer on the request, and that row is what a later contact lookup checks. The
  two are separate tables precisely so approval and exposure cannot be conflated.
- **A phone number is fetched only for a GRANTED, unexpired row** (R9). A seller with
  no grant is refused before auth-service is called at all, so an unauthorised request
  never reaches the service that holds the number.
- **Grants are per seller.** Approving one seller's offer tells a rival seller nothing,
  even on the same request.
- **The decision and its grants are one unit.** The offer is read and the participants
  fetched before anything is written; the local rows are then written and the remote
  status flipped inside a single transaction that commits last. If offer-service
  refuses the relay, everything rolls back and the offer is left `PENDING` rather than
  approved with no grants behind it. The remaining window — a commit failing after
  offer-service accepted the `PATCH` — needs an outbox to close properly, and is the
  one place this service can diverge from offer-service.
- **One decision per offer** (R7). Enforced by a unique constraint on `offer_id` rather
  than a read-then-write check, which two concurrent approvals would both pass. A
  second decision is a 409, never a silent overwrite of the audit record.
- **Revoking is a status change, not a delete.** The row is the audit history of who
  was allowed to reach whom; deleting it would erase the record this service exists to
  keep. It takes effect on the next contact lookup.
- **Identity is never accepted from the caller.** The admin on a decision comes from
  the token's `sub`, and the seller on a contact lookup is resolved through
  seller-service. The verdict comes from which route was called, so a body carrying
  `decision` is rejected outright, not ignored.
- **No phone number is ever stored here** (R10). `admin_contact_db` holds ids and
  grants; every number is fetched from auth-service per call and passed straight
  through.

## Notification Service

Records and delivers the events of spec section 18. It is not the source of truth for
any business data, and that shows in its shape: it never resolves an identity or reads
another service's data. A producer has already decided who the recipient is and what
the message says by the time the event arrives, so this service takes a `userId`, a
rendered title and a message, and owns only the delivery and read state that follow.

Events reach it over RabbitMQ rather than a direct HTTP call, so a producer never waits
on it and a notification is not lost the moment it is slow. The internal HTTP API named
in section 13 is still served and runs the same code, so the two paths cannot drift.
The full contract - topology, envelope, delivery guarantees - is in `docs/events.md`.

| Method | Endpoint | Who |
|---|---|---|
| `GET` | `/api/notifications/me` | any authenticated user |
| `GET` | `/api/notifications/me/unread-count` | any authenticated user |
| `PATCH` | `/api/notifications/{notificationId}/read` | the recipient |
| `POST` | `/internal/notifications` | internal key |
| `POST` | `/internal/notifications/bulk` | internal key |

| Event | Routing key | Produced by | Recipient |
|---|---|---|---|
| `REQUEST_JOINED` | `request.joined` | Request | the customer who acted |
| `NEW_OFFER` | `offer.created` | Offer | every ADMIN |
| `OFFER_APPROVED` | `offer.approved` | Admin/Contact | the seller |
| `OFFER_REJECTED` | `offer.rejected` | Admin/Contact | the seller |
| `CONTACT_ACCESS_GRANTED` | `contact.access.granted` | Admin/Contact | the seller |
| `REQUEST_CLOSED` | `request.closed` | Request | every participant |

Rules worth knowing:

- **The status column always reflects a real outcome.** `IN_APP` needs no provider —
  the row *is* the delivery, so storing it sends it and it goes straight to `SENT`.
  `EMAIL`, `SMS` and `PUSH` have no provider wired in this MVP, so they are left
  `PENDING` for a dispatcher rather than being marked `SENT` on the strength of nothing
  having happened. A `CHECK` constraint ties `sent_at` to the status so the two cannot
  drift.
- **Another user's notification answers 404, not 403.** A 403 would confirm the id
  exists, which turns the read endpoint into an oracle for enumerating other people's
  notifications. The ownership test is part of the lookup rather than a separate
  authorisation step, so no code path can return someone else's row.
- **Marking read is idempotent**, unlike the decide-and-revoke calls elsewhere in the
  platform that answer 409 on a repeat. Those are decisions, where a second attempt
  means the caller believes something no longer true. Reading is not a decision — two
  devices syncing one inbox is ordinary — so a repeat returns the notification
  unchanged. A notification that was never delivered cannot be read at all.
- **The unread count excludes `FAILED`.** A notification that never reached the user is
  an operational problem, not an unread message.
- **A fan-out is all or nothing.** `REQUEST_CLOSED` reaching some participants and not
  others, with no record of which, is worse than reaching none: the producer could not
  safely retry it. One transaction means a failed call is simply repeated.
- **The event set is open by design.** Section 13 introduces the list with "Examples",
  so the column is shape-checked (`^[A-Z][A-Z0-9_]*$`) rather than constrained to an
  enum — adding an event to the platform never means migrating this table.
- **There is no public write route.** Creating is service-to-service only, so a user
  cannot manufacture their own notifications.
- **An event is applied exactly once.** AMQP redelivers after a consumer crash or a
  missed ack, so the `eventId` is inserted into `processed_event` in the same
  transaction as the notifications it produced. A redelivery hits the primary key and
  is acked without writing anything twice. The dedupe is the insert, not a prior
  `SELECT`: two deliveries can be in flight at once, and a check-then-act would let
  both pass before either committed.
- **A message that cannot be processed is dead-lettered, never requeued in place.** A
  poison message requeued to the same queue is redelivered immediately and forever,
  which takes the consumer down with it.

### The outbox: why a broker outage costs latency, not notifications

Nothing is published inline. Every producer writes the event to its own
`notification_outbox` table **inside the transaction that caused it**, and a relay
drains that table to the broker. The event and the business change are one write, so
there is no window in which a request is joined but the notification has vanished.

```
  business tx ──┬── the change (request, offer, decision)
                └── notification_outbox row        ← one commit

  relay loop  ──── SELECT … WHERE published_at IS NULL
                   FOR UPDATE SKIP LOCKED          ← safe with several replicas
                   publish → mark published_at
```

- **The `eventId` is fixed when the row is written**, not when it is published. A relay
  that dies between publishing and the commit that marks the row sent republishes it
  with the same id, which the consumer recognises as a redelivery — so at-least-once
  relaying stays exactly-once in effect.
- **The relay polls** rather than using `LISTEN`/`NOTIFY`. A subscription would be lower
  latency but would silently miss rows written while its connection was down, which is
  the one case the outbox exists for. Producers nudge it on commit, so the interval only
  matters when a nudge is lost.
- **The dial is bounded.** `amqp.Dial` defaults to a 30-second timeout, and a stopped
  broker on a Docker network blackholes the connection rather than refusing it. Left
  alone, that turned every request-creating call into a 16-second wait. The dial is
  capped at 700ms and a publish at 2s.

What is still *not* transactional: Admin/Contact's `Decide` relays the offer status to
offer-service over HTTP, and moves the request to `OFFER_APPROVED` in a second call
after the commit. Those are commands to other services rather than notifications, so
the outbox does not cover them. A command outbox would, at the cost of making approval
asynchronous — an admin would stop learning from the response whether offer-service had
accepted the decision, which is a worse trade than the window it closes.

## Running Locally

The root compose file runs the whole system — seven services, their seven databases, a broker
and the gateway — on one network, with the shared secrets pinned in one place:

```bash
docker compose up --build
curl localhost:8080/api/requests      # everything public goes through :8080
```

**Only 8080 is published.** The services have no `ports:` mapping, so the security boundary
in development is the same one an Ingress enforces in production rather than a rule that
only holds once deployed. Reaching a service directly — which the `/internal` and
`/actuator` calls need, since neither is routable through the gateway — is opt-in:

```bash
docker compose -f docker-compose.yml -f docker-compose.direct.yml up   # republishes 8081-8087
```

That is the compose equivalent of `kubectl port-forward`: forgetting it fails closed.

### API documentation

Every service generates an OpenAPI document from its own code and publishes it at
`/v3/api-docs`; the gateway serves them under one Swagger UI:

```
http://localhost:8080/docs        one page, a dropdown over all seven services
```

Paste the `accessToken` from `POST /api/auth/login` into **Authorize** once and it
persists across the dropdown, so a single login covers every spec. **Try it out** calls
the API on the same origin the page came from, which is why it needs no CORS exemption
and no second port.

| Stack | Generated by | Regenerated |
|---|---|---|
| Java (auth, customer, seller) | springdoc, `paths-to-match: /api/**` | on startup |
| Python (offer, notification) | FastAPI, `/internal` excluded from the schema | on startup |
| Go (request, admin) | `swag init` from handler annotations | by the command below |

Nothing here is a second description of the API that can fall out of step with a
handler - which is why the Go pair, the only ones with a manual step, keep their
generated document in the tree:

```bash
cd request-service    # or admin-service
swag init --v3.1 -g cmd/server/main.go -o internal/docs -ot json
```

`--v3.1` because swag's default is Swagger 2.0, where Authorize expects `Bearer <token>`
rather than the token - the other five publish OpenAPI 3, and one odd one out is a
papercut on every call. `-ot json` because swag's `docs.go` would pull the swag runtime
into the binary to hand back a string the package can embed for free.

No spec describes `/internal` or `/actuator`. Neither has a route through the gateway,
so publishing them would advertise an API that nothing outside the cluster can call.

### Testing a running stack

`scripts/smoke.sh` walks the whole platform end to end in the order the spec's interaction
flows describe — a request created and joined, a seller offering against the aggregated
demand, an admin approving, and only then a phone number becoming reachable — then checks
that `/internal` and `/actuator` have no route and that CORS preflight is answered at the
edge. Every call goes to `:8080`; if any step needed a service port, the gateway would be
wrong. It needs only curl and python3:

```bash
docker compose up --build      # in another terminal, wait for it to settle
./scripts/smoke.sh             # BASE=http://localhost:8080 by default
```

Each run registers fresh accounts off a timestamp, so it is re-runnable without a reset.
It exits non-zero on the first failed expectation, which makes it usable as a post-deploy
check and not only as something to read.

For the exhaustive pass, run the Postman collection with newman — 116 requests including
the negative cases. The `/internal` and `/actuator` folders need the direct override
above, since neither is reachable through the gateway:

```bash
docker compose -f docker-compose.yml -f docker-compose.direct.yml up
docker run --rm --network host -v "$PWD/postman":/etc/newman postman/newman:alpine \
  run marketplace.postman_collection.json
```

The three Java services also compose independently (`cd auth-service && docker compose
up --build`); request-service and offer-service do not, since each only runs alongside
the services it resolves identities through.

Copy `.env.example` to `.env` first when running a service on its own. `JWT_SECRET` and
`INTERNAL_API_KEY` **must be identical across every service** — tokens are validated
locally by each service, and the internal key is a single shared secret.

### Build and test

```bash
mvn test                        # all Maven modules, ~60s
mvn -pl auth-service test       # one module's tests
mvn clean install -DskipTests   # build without testing
```

The two Go modules build and test on their own, outside the Maven reactor:

```bash
cd request-service              # or: cd admin-service
go test ./...                   # unit + integration; starts its own Postgres
go build ./...
DATABASE_URL=postgres://postgres:postgres@localhost:5435/request_db?sslmode=disable \
  go test ./...                 # reuse a running Postgres instead of a container
DATABASE_URL=postgres://postgres:postgres@localhost:5437/admin_contact_db?sslmode=disable \
  go test ./...                 # the same switch for admin-service, on its own port
```

Regenerate the data layer after changing any SQL under `internal/db/`:

```bash
cd request-service && sqlc generate     # or admin-service; both carry their own sqlc.yaml
# without a local sqlc: docker run --rm -v "$PWD":/src -w /src sqlc/sqlc generate
```

The Python module uses [uv](https://docs.astral.sh/uv/), which manages its own pinned
interpreter — no system Python or virtualenv setup required:

```bash
cd offer-service                # or: cd notification-service
uv sync --all-groups            # create .venv from uv.lock
uv run pytest                   # unit + integration; starts its own Postgres
uv run ruff check . && uv run ruff format --check .
uv run uvicorn app.main:app --port 8085 --reload

DATABASE_URL=postgres://postgres:postgres@localhost:5436/offer_db \
  uv run pytest                 # reuse a running Postgres instead of a container
DATABASE_URL=postgres://postgres:postgres@localhost:5438/notification_db \
  uv run pytest                 # the same switch for notification-service
```

After changing a SQLAlchemy model, write the matching migration — the model alone never
alters the schema:

```bash
cd offer-service && uv run alembic revision --autogenerate -m "describe the change"
```

Heap is capped deliberately — `-Xmx768m` for surefire forks (root `pom.xml`) and for
Maven itself (`.mvn/jvm.config`). Without those caps each JVM claims a quarter of
physical RAM, which on an 8 GB machine gets the reactor OOM-killed while a Testcontainers
PostgreSQL is resident. Each module starts one container per JVM, shared across its test
classes.

## Documentation

- `docs/technical-specification.pdf` — the full specification (business rules, service
  boundaries, interaction flows).
- `docs/events.md` — the AMQP contract: topology, envelope, delivery guarantees, and
  which service produces which event.
- `http://localhost:8080/docs` — the aggregated Swagger UI, generated per service.
- `infrastructure/docker/gateway/nginx.conf` — the routing table: spec section 19's nine
  public routes, and the default-deny that keeps `/internal` off the edge. A Kubernetes
  Ingress later expresses the same nine prefixes; `pathType: Prefix` means exactly what the
  `(/|$)` boundary means here.
- `postman/marketplace.postman_collection.json` — 116 requests covering every service,
  the internal APIs, negative cases and health. The 82 `/api` requests go through the
  gateway on `{{gatewayUrl}}`; the 26 `/internal` and 8 `/actuator` requests address a
  service directly and need `docker-compose.direct.yml`. Run it against a live stack with
  `docker run --rm --network host -v "$PWD/postman":/etc/newman postman/newman:alpine
  run marketplace.postman_collection.json`. It is re-runnable: the customer identity is
  generated per run, because the account update step rewrites that user's email.
- Open work is tracked in [GitHub Issues](https://github.com/hmidaayoub/marketplace-microservices/issues),
  labelled by service and priority.
