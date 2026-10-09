# syntax=docker/dockerfile:1
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
LABEL org.opencontainers.image.title="Cetacean" \
      org.opencontainers.image.description="A real-time observability dashboard for Docker Swarm clusters." \
      org.opencontainers.image.licenses="GPL-3.0" \
      org.opencontainers.image.url="https://github.com/radiergummi/cetacean"

ARG TARGETARCH

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --link binaries/linux/${TARGETARCH}/cetacean /usr/local/bin/cetacean

EXPOSE 9000
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
  CMD ["cetacean", "healthcheck"]
ENTRYPOINT ["cetacean"]
