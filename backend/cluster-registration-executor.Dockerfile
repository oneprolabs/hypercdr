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
RUN go build -trimpath -ldflags="-s -w -X hypercdr-platform/platform/backend/internal/buildinfo.Version=${VERSION} -X hypercdr-platform/platform/backend/internal/buildinfo.GitCommit=${GIT_COMMIT} -X hypercdr-platform/platform/backend/internal/buildinfo.BuildTime=${BUILD_TIME}" -o /out/cluster-registration-executor ./cmd/cluster-registration-executor \
    && go build -trimpath -ldflags="-s -w" -o /out/curl ./cmd/registration-curl

FROM golang:1.25.13-bookworm AS kubectl-downloader
ARG KUBECTL_VERSION=v1.28.15
ARG KUBECTL_DOWNLOAD_MAX_TIME=300
RUN set -eu; \
    curl -fL --retry 5 --retry-delay 2 --connect-timeout 10 --max-time "${KUBECTL_DOWNLOAD_MAX_TIME}" \
      "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" -o /kubectl; \
    checksum="$(curl -fsSL --retry 3 --connect-timeout 10 --max-time 60 \
      "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl.sha256")"; \
    printf '%s  /kubectl\n' "${checksum}" | sha256sum -c -; \
    chmod 0755 /kubectl

FROM debian:bookworm-slim
COPY --from=kubectl-downloader /kubectl /usr/local/bin/kubectl
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/cluster-registration-executor /usr/local/bin/cluster-registration-executor
COPY --from=builder /out/curl /usr/local/bin/curl
EXPOSE 18082
ENTRYPOINT ["/usr/local/bin/cluster-registration-executor"]
