# syntax=docker/dockerfile:1
FROM golang:1.25.13-bookworm AS builder

WORKDIR /src
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY} GOTOOLCHAIN=local CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w" -o /out/comm-agent ./cmd/comm-agent

FROM scratch
COPY --from=builder /out/comm-agent /comm-agent
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
USER 65532:65532
ENTRYPOINT ["/comm-agent"]
