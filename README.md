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
| Offer | 8085 | `offer_db` | Not started |
| Admin/Contact | 8086 | `admin_contact_db` | Not started |
| Notification | 8087 | `notification_db` | Not started |
| API Gateway | 8080 | — | Not started |

82 tests pass across the four Maven modules, and 46 across the Go module.

The platform is deliberately polyglot from here on: Auth, Customer and Seller are Spring
Boot, Request is Go. They interoperate only through HTTP and a shared JWT secret, which
is the boundary the architecture already assumed - no service reads another's database,
so the language behind an endpoint is not something its callers can observe.

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
| Containers | Docker, Docker Compose | Multi-stage builds, non-root runtime user |

### Go stack (request-service)

| Concern | Choice | Notes |
|---|---|---|
| Language | Go 1.25 | |
| Router | chi v5 | Middleware chain mirrors the Spring filter chain |
| **Data access** | **sqlc + pgx v5** | SQL is hand-written; sqlc generates typed Go from it |
| Schema migrations | golang-migrate | `internal/db/migrations`, embedded in the binary via `go:embed` |
| JWT | golang-jwt v5 | Verifies tokens minted by auth-service's jjwt |
| Integration testing | testcontainers-go (PostgreSQL 15) | Real Postgres, real migrations |

Planned but not yet wired in: RabbitMQ, Kubernetes, Terraform, GitHub Actions,
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
└── request-service/    Go module, independent of the Maven reactor
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
`RestTemplate` interceptor. These endpoints must never be routed by the public gateway.
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
| `GET` | `/internal/requests/{requestId}` | internal key |
| `GET` | `/internal/requests/{requestId}/demand` | internal key |
| `GET` | `/internal/requests/{requestId}/participants` | internal key |

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
  `/internal/requests/{id}/participants`, which is what Admin/Contact will use to grant
  ContactAccess — so a seller browsing demand cannot enumerate the customers behind it.

## Running Locally

The root compose file runs the whole system — four services and their four databases —
on one network, with the shared secrets pinned in one place:

```bash
docker compose up --build
```

The three Java services also compose independently (`cd auth-service && docker compose
up --build`); request-service does not, since it only runs alongside customer-service.

Copy `.env.example` to `.env` first when running a service on its own. `JWT_SECRET` and
`INTERNAL_API_KEY` **must be identical across every service** — tokens are validated
locally by each service, and the internal key is a single shared secret.

### Build and test

```bash
mvn test                        # all Maven modules, ~60s
mvn -pl auth-service test       # one module's tests
mvn clean install -DskipTests   # build without testing
```

The Go module builds and tests on its own, outside the Maven reactor:

```bash
cd request-service
go test ./...                   # unit + integration; starts its own Postgres
go build ./...
DATABASE_URL=postgres://postgres:postgres@localhost:5435/request_db?sslmode=disable \
  go test ./...                 # reuse a running Postgres instead of a container
```

Regenerate the data layer after changing any SQL under `internal/db/`:

```bash
cd request-service && sqlc generate     # or: docker run --rm -v "$PWD":/src -w /src sqlc/sqlc generate
```

Heap is capped deliberately — `-Xmx768m` for surefire forks (root `pom.xml`) and for
Maven itself (`.mvn/jvm.config`). Without those caps each JVM claims a quarter of
physical RAM, which on an 8 GB machine gets the reactor OOM-killed while a Testcontainers
PostgreSQL is resident. Each module starts one container per JVM, shared across its test
classes.

## Documentation

- `docs/` — the full technical specification (business rules, service boundaries,
  interaction flows).
- `postman/marketplace.postman_collection.json` — 54 requests covering every service,
  the internal APIs, negative cases and health. Run it against a live stack with
  `docker run --rm --network host -v "$PWD/postman":/etc/newman postman/newman:alpine
  run marketplace.postman_collection.json`. It is re-runnable: the customer identity is
  generated per run, because the account update step rewrites that user's email.
- Open work is tracked in [GitHub Issues](https://github.com/hmidaayoub/marketplace-microservices/issues),
  labelled by service and priority.
