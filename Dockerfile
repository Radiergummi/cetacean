# syntax=docker/dockerfile:1

FROM node:24-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS frontend
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

FROM golang:1.27-alpine@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS backend
ARG VERSION=dev
ARG COMMIT=unknown
WORKDIR /app
COPY --link go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY --link . .
COPY --from=frontend /app/frontend/dist ./frontend/dist
COPY --from=frontend /app/frontend/dist-widgets ./frontend/dist-widgets
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -ldflags "-s -w \
    -X github.com/radiergummi/cetacean/internal/version.Version=${VERSION} \
    -X github.com/radiergummi/cetacean/internal/version.Commit=${COMMIT} \
    -X github.com/radiergummi/cetacean/internal/version.Date=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o cetacean .

FROM scratch
COPY --from=backend /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=backend /app/cetacean /usr/local/bin/cetacean
EXPOSE 9000
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
CMD ["cetacean", "healthcheck"]
ENTRYPOINT ["cetacean"]
