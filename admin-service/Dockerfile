# Build context is the REPOSITORY ROOT, matching the other services so one compose
# file can build them all the same way:
#   docker build -f admin-service/Dockerfile .
#
# Unlike the Java modules this one has no shared parent to resolve - the Go module is
# self-contained - but the context stays at the root for consistency.
FROM golang:1.25-alpine AS builder
WORKDIR /build

# Manifests first: dependency download caches independently of source changes.
COPY admin-service/go.mod admin-service/go.sum ./
RUN go mod download

COPY admin-service/ ./

# CGO off produces a static binary, so the runtime image needs no libc and the
# migrations travel inside the binary via go:embed. -trimpath keeps build paths out of
# the artifact; -s -w drop the symbol and DWARF tables.
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/admin-service ./cmd/server

FROM alpine:3.20
WORKDIR /app

# busybox wget backs the compose healthcheck; ca-certificates lets the service call
# other services over TLS once anything sits behind one.
RUN apk add --no-cache ca-certificates \
    && addgroup -S marketplace && adduser -S admin -G marketplace

COPY --from=builder /out/admin-service /app/admin-service
RUN chown admin:marketplace /app/admin-service

USER admin
EXPOSE 8086
ENTRYPOINT ["/app/admin-service"]
