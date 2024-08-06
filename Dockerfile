FROM alpine:latest as certs
RUN apk update && apk add ca-certificates

FROM cgr.dev/chainguard/busybox:latest
COPY --from=certs /etc/ssl/certs /etc/ssl/certs

COPY ns1_exporter /usr/bin/ns1_exporter

USER nobody
ENTRYPOINT ["/usr/bin/ns1_exporter"]
