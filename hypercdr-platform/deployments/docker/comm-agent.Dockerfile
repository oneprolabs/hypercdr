ARG GOLANG_IMAGE=golang:1.24
ARG DISTROLESS_IMAGE=gcr.io/distroless/static-debian12:nonroot
ARG GOPROXY=
ARG GOSUMDB=

FROM ${GOLANG_IMAGE} AS builder
ARG GOPROXY
ARG GOSUMDB

WORKDIR /src
COPY agent/comm-agent/go.mod agent/comm-agent/go.sum ./
RUN if [ -n "${GOPROXY}" ] || [ -n "${GOSUMDB}" ]; then \
      GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" GOSUMDB="${GOSUMDB:-sum.golang.org}" go mod download; \
    else \
      go mod download; \
    fi

COPY agent/comm-agent/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/comm-agent ./cmd/comm-agent

FROM ${DISTROLESS_IMAGE}

COPY --from=builder /out/comm-agent /comm-agent
USER nonroot:nonroot
ENTRYPOINT ["/comm-agent"]
