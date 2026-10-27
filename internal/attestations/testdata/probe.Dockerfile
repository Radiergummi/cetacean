FROM alpine:3.20 AS deps
RUN apk add --no-cache jq

FROM alpine:3.20
RUN apk add --no-cache curl
RUN dd if=/dev/zero of=/big bs=1M count=5
COPY --from=deps /usr/bin/jq /usr/local/bin/jq
COPY hello.txt /hello.txt
RUN echo done > /done
