# Docker Development

Start local PostgreSQL:

```bash
docker compose -f docker-compose.dev.yml up -d postgres
```

If the image is not present locally, Docker must be able to pull `postgres:16` from a registry.

Run the platform backend with PostgreSQL:

```bash
cd backend
HCDR_DATABASE_URL='postgres://hypercdr:hypercdr@127.0.0.1:15432/hypercdr?sslmode=disable' \
HCDR_HTTP_ADDR='127.0.0.1:18080' \
go run -buildvcs=false ./cmd/platform-api
```

The backend runs embedded SQL migrations automatically when `HCDR_DATABASE_URL` is set.

Run migrations explicitly:

```bash
cd backend
HCDR_DATABASE_URL='postgres://hypercdr:hypercdr@127.0.0.1:15432/hypercdr?sslmode=disable' \
go run -buildvcs=false ./cmd/platform-migrate
```
