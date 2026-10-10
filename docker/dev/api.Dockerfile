FROM golang:1.25.13-bookworm AS builder
WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /src/backend
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY} CGO_ENABLED=0
RUN go mod download
COPY backend/ ./
RUN go build -trimpath -o /out/platform-api ./cmd/platform-api && \
    go build -trimpath -o /out/platform-migrate ./cmd/platform-migrate && \
    go build -trimpath -o /out/curl ./cmd/registration-curl
FROM debian:bookworm-slim
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/ /usr/local/bin/
CMD ["/bin/sh", "-c", "platform-migrate && exec platform-api"]
