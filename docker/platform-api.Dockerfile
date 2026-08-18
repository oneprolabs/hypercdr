ARG NODE_IMAGE=node:22-bookworm-slim
ARG GOLANG_IMAGE=golang:1.24-bookworm
ARG DEBIAN_IMAGE=debian:bookworm-slim
ARG NPM_REGISTRY=
ARG GOPROXY=
ARG GOSUMDB=

FROM ${NODE_IMAGE} AS frontend-builder
ARG NPM_REGISTRY

WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN if [ -n "${NPM_REGISTRY}" ]; then npm ci --registry="${NPM_REGISTRY}"; else npm ci; fi
COPY frontend/ ./
ENV HCDR_FRONTEND_OUT_DIR=/src/frontend/dist
RUN npm run build

FROM ${GOLANG_IMAGE} AS backend-builder
ARG GOPROXY
ARG GOSUMDB

WORKDIR /src
COPY backend/go.mod backend/go.sum ./backend/
WORKDIR /src/backend
RUN if [ -n "${GOPROXY}" ] || [ -n "${GOSUMDB}" ]; then \
      GOPROXY="${GOPROXY:-https://proxy.golang.org,direct}" GOSUMDB="${GOSUMDB:-sum.golang.org}" go mod download; \
    else \
      go mod download; \
    fi

WORKDIR /src
COPY backend/ ./backend/
WORKDIR /src/backend
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/platform-api ./cmd/platform-api
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/platform-migrate ./cmd/platform-migrate

FROM ${DEBIAN_IMAGE}

COPY --from=backend-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

COPY --from=backend-builder /out/platform-api /usr/local/bin/platform-api
COPY --from=backend-builder /out/platform-migrate /usr/local/bin/platform-migrate
COPY --from=frontend-builder /src/frontend/dist /opt/hypercdr/frontend

ENV HCDR_HTTP_ADDR=0.0.0.0:18080
ENV HCDR_FRONTEND_DIR=/opt/hypercdr/frontend

EXPOSE 18080
ENTRYPOINT ["/usr/local/bin/platform-api"]
