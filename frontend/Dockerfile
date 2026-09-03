# Build context is the REPOSITORY ROOT, matching every other service so one compose file
# builds them all the same way:
#   docker build -f frontend/Dockerfile .
#
# Two stages, and the second carries no toolchain: what ships is a directory of static
# files and an nginx to hand them over. Node exists only to produce that directory.
FROM node:24-alpine AS build

WORKDIR /app

# Manifests first, so a source change does not reinstall the dependency tree.
COPY frontend/package.json frontend/package-lock.json ./
# ci, not install: exactly what the lockfile pins, and a failure if it has drifted from
# package.json. An image built from a tree nobody has is worse than a build that stops.
RUN npm ci

COPY frontend/ ./

# Same origin, so no absolute API base is baked in. The gateway serves this bundle and
# the API from one port, which is what makes the app's requests relative - and what makes
# the CORS allowance at the edge a development convenience rather than part of the
# deployed edge. `npm run dev` still reads its own .env and talks to :8080 cross-origin.
ENV VITE_API_URL=""

RUN npm run build

# The same nginx the gateway runs, pinned the same way: two images serving over one
# network should not differ in the thing they both are.
#
# This is the one image of the eight that does not drop privileges, and the reason
# is the entrypoint rather than an omission: stock nginx starts as root to bind :80
# and to write its pid and cache directories. Fixing it properly means the
# unprivileged image on a high port, which moves the gateway upstream and this
# file's own HEALTHCHECK with it. Tracked, with the same expiry, as DS-0002 in
# .trivyignore.yaml - both scanners have to be told, so both are told in writing.
# checkov:skip=CKV_DOCKER_3: nginx needs root to bind :80; see .trivyignore.yaml DS-0002
FROM nginx:1.27-alpine

COPY --from=build /app/dist /usr/share/nginx/html
COPY frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

# 127.0.0.1 rather than localhost, for the reason the gateway's own probe gives: inside
# the container localhost resolves to ::1 first and nginx listens on IPv4 only.
HEALTHCHECK --interval=10s --timeout=5s --retries=6 --start-period=5s \
    CMD wget -qO- http://127.0.0.1/ >/dev/null || exit 1
