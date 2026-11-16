# syntax=docker/dockerfile:1
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS certs
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
