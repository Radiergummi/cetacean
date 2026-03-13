# syntax=docker/dockerfile:1
FROM alpine:3.23
LABEL org.opencontainers.image.title="Cetacean" \
      org.opencontainers.image.description="A real-time observability dashboard for Docker Swarm clusters." \
      org.opencontainers.image.license="GPL-3.0" \
      org.opencontainers.image.url="https://github.com/radiergummi/cetacean"

ARG TARGETARCH

RUN apk add --no-cache ca-certificates
COPY --link binaries/linux/${TARGETARCH}/cetacean /usr/local/bin/cetacean

EXPOSE 9000
HEALTHCHECK --interval=10s --timeout=3s --start-period=30s --retries=3 \
  CMD wget -qO- http://localhost:9000/-/health || exit 1
ENTRYPOINT ["cetacean"]
