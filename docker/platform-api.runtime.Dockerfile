FROM debian:bookworm-slim

COPY ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY platform-api /usr/local/bin/platform-api
COPY platform-migrate /usr/local/bin/platform-migrate

ENV HCDR_HTTP_ADDR=0.0.0.0:18080

EXPOSE 18080
ENTRYPOINT ["/usr/local/bin/platform-api"]
