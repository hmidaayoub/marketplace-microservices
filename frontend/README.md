# Marketplace frontend

React + TypeScript, shadcn/ui on Tailwind v4 for the interface, Redux Toolkit for state.
Every call goes to the gateway on `:8080` — the platform publishes no other port, so
there is one base URL and nothing to configure per service.

```bash
npm install
npm run dev        # http://localhost:5173
```

## The component layer is shadcn/ui

`src/components/ui/` is vendored shadcn source — it is ours to edit, not a dependency to
upgrade. Add to it with the CLI rather than by hand:

```bash
npx shadcn@latest add <component>
```

Everything above it lives in `src/components/`: `app-layout` (the role-aware shell),
`protected-route`, `auth-shell`, `status-badge`, `error-alert`, `page-header`. Imports go
through the `@/` alias, which `vite.config.ts` and `tsconfig.app.json` both have to agree
on.

The theme is the shadcn token set in `src/index.css` with two changes, both for reasons
the domain forces:

- **`primary` is blue, not the preset's near-black.** The one irreversible action in the
  platform is an admin approving an offer, and `destructive` has to stay visually
  distinct from "the main button on this screen" for that to read.
- **`success` and `warning` are added.** The domain has statuses that are neither —
  a `PENDING` offer is the healthy state of a new offer, and colouring it with the stock
  three would make it look like a fault. `status-badge.tsx` holds the whole mapping.

Light and dark both ship; `next-themes` owns the `.dark` class and the toggle is in the
header.

The dev server runs on 5173 deliberately: it is one of the origins the gateway's CORS
allowlist names, so the API is reachable from a cold start with no proxy in between.
`:3000` and `:4200` work too.

## Types are generated, not written

```bash
npm run codegen    # needs the stack running
```

This pulls all seven OpenAPI documents from the gateway and regenerates
`src/api/schema/`. Every type the app uses is derived from those, so a backend change
that alters a response surfaces as a compile error rather than a runtime surprise — it
caught a wrong assumption about the contact-access shape on the first day.

## Testing

```bash
npm run e2e        # drives register -> profile -> create a request in a real browser
```

It exists because "account creation does not work" was diagnosed three times from the
outside — the API passed, the store passed, and the fault was still in front of the
user. This is the only check that sees what they see. Failures leave a screenshot in
`e2e/`.

For the backend, `../scripts/smoke.sh` answers whether the platform is up before you
debug the UI. A **502** from any call means a service is down, not that your code is
wrong.

## Two things the backend's design forces on the UI

**Signing up is two steps.** Registering creates a *user*; it does not create the
CUSTOMER or SELLER profile that request- and offer-service resolve callers through.
Until it exists every business write answers 403, so `ProtectedRoute` tracks profile
state and routes to `/profile` rather than letting people hit that wall. An account
created without its profile is a normal state to be in, and signing in resumes it.

**Notifications are eventually consistent.** Producers write events to an outbox inside
the business transaction and a relay publishes them on a two-second tick, so an action's
notification does not exist when the action returns. The badge and inbox poll instead of
refetching immediately.
