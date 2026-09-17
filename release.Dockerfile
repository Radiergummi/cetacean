# syntax=docker/dockerfile:1
FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b AS certs
RUN apk add --no-cache ca-certificates

FROM scratch
LABEL org.opencontainers.image.title="Cetacean" \
      org.opencontainers.image.description="A real-time observability dashboard for Docker Swarm clusters." \
      org.opencontainers.image.license="GPL-3.0" \
      org.opencontainers.image.url="https://github.com/radiergummi/cetacean"

ARG TARGETARCH

COPY --from=certs /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --link binaries/linux/${TARGETARCH}/cetacean /usr/local/bin/cetacean

EXPOSE 9000
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
  CMD ["cetacean", "healthcheck"]
ENTRYPOINT ["cetacean"]
