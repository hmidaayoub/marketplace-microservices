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
| Request | 8084 | `request_db` | Not started |
| Offer | 8085 | `offer_db` | Not started |
| Admin/Contact | 8086 | `admin_contact_db` | Not started |
| Notification | 8087 | `notification_db` | Not started |
| API Gateway | 8080 | — | Not started |

63 tests currently pass across the four modules.

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
└── seller-service/
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

## Running Locally

Each service composes independently:

```bash
cd auth-service && docker compose up --build
```

Copy `.env.example` to `.env` first. `JWT_SECRET` and `INTERNAL_API_KEY` **must be
identical across every service** — tokens are validated locally by each service, and the
internal key is a single shared secret.

### Build and test

```bash
mvn clean install -DskipTests   # all modules
mvn -pl auth-service test       # one module's tests
```

Run integration suites **one module at a time**. Each spins up its own PostgreSQL
container, and running all of them in a single reactor invocation exhausts memory on an
8 GB machine.

## Documentation

- `docs/` — the full technical specification (business rules, service boundaries,
  interaction flows).
- Open work is tracked in [GitHub Issues](https://github.com/hmidaayoub/marketplace-microservices/issues),
  labelled by service and priority.
