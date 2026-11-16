# syntax=docker/dockerfile:1

FROM node:24-alpine@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5 AS frontend
WORKDIR /app
# The workspace resolves from the root, so the manifests that describe it have
# to arrive before the install and ahead of the sources that invalidate it.
COPY --link package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY --link frontend/package.json frontend/
RUN npm install -g "pnpm@$(node -p 'require("./package.json").packageManager.split("@")[1]')"
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile --filter frontend...
COPY --link frontend/ frontend/
WORKDIR /app/frontend
RUN pnpm build
# MCP Apps widget bundles; main.go embeds frontend/dist-widgets.
RUN pnpm build:widgets

FROM golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS backend
ARG VERSION=dev
ARG COMMIT=unknown
# Pass the commit time (git log -1 --format=%ct) for a reproducible build; the
# build date falls back to the clock without it.
ARG SOURCE_DATE_EPOCH
WORKDIR /app
COPY --link go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY --link . .
COPY --from=frontend /app/frontend/dist ./frontend/dist
COPY --from=frontend /app/frontend/dist-widgets ./frontend/dist-widgets
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w \
    -X github.com/radiergummi/cetacean/internal/version.Version=${VERSION} \
    -X github.com/radiergummi/cetacean/internal/version.Commit=${COMMIT} \
    -X github.com/radiergummi/cetacean/internal/version.Date=$(date -u ${SOURCE_DATE_EPOCH:+-d @$SOURCE_DATE_EPOCH} +%Y-%m-%dT%H:%M:%SZ)" \
    -o cetacean .

FROM scratch
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=backend /app/cetacean /usr/local/bin/cetacean
EXPOSE 9000
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
CMD ["cetacean", "healthcheck"]
ENTRYPOINT ["cetacean"]
