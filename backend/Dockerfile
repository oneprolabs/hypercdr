# syntax=docker/dockerfile:1
FROM golang:1.25.13-bookworm AS builder

WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY} GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION
ARG GIT_COMMIT
ARG BUILD_TIME
RUN go build -trimpath -ldflags="-s -w -X hypercdr-platform/platform/backend/internal/buildinfo.Version=${VERSION} -X hypercdr-platform/platform/backend/internal/buildinfo.GitCommit=${GIT_COMMIT} -X hypercdr-platform/platform/backend/internal/buildinfo.BuildTime=${BUILD_TIME}" -o /out/platform-api ./cmd/platform-api \
    && go build -trimpath -ldflags="-s -w" -o /out/platform-migrate ./cmd/platform-migrate

FROM debian:bookworm-slim
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/platform-api /usr/local/bin/platform-api
COPY --from=builder /out/platform-migrate /usr/local/bin/platform-migrate
ENV HCDR_HTTP_ADDR=0.0.0.0:18080
EXPOSE 18080
ENTRYPOINT ["/usr/local/bin/platform-api"]
