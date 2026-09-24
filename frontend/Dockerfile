# syntax=docker/dockerfile:1
FROM node:22-alpine AS builder

WORKDIR /app
ARG NPM_REGISTRY=https://registry.npmjs.org
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --registry="${NPM_REGISTRY}"
COPY frontend/ ./
ARG VERSION
ARG RELEASE_DATE
RUN VITE_HCDR_RELEASE_VERSION="${VERSION}" VITE_HCDR_RELEASE_DATE="${RELEASE_DATE}" \
    npm run build -- --outDir /app/dist --emptyOutDir \
    && css="$(find /app/dist/assets -maxdepth 1 -name '*.css' -print -quit)" \
    && test -s "${css}" \
    && for utility in '.grid{' '.flex{' '.items-center{' '.px-5{' '.py-4{'; do grep -Fq "${utility}" "${css}" || exit 1; done

FROM nginx:1.27-alpine
COPY docker/nginx/default.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist/ /usr/share/nginx/html/
RUN sed -i '/server {/a\\    gzip on;\n    gzip_vary on;\n    gzip_min_length 1024;\n    gzip_types text/css application/javascript application/json image/svg+xml;' /etc/nginx/conf.d/default.conf \
    && sed -i '/location \/ {/i\\    location ~* \\.(?:css|js|svg|ico|woff2?)$ {\n        expires 30d;\n        add_header Cache-Control "public, max-age=2592000, immutable";\n    }' /etc/nginx/conf.d/default.conf
RUN chmod -R a+rX /usr/share/nginx/html
EXPOSE 80
