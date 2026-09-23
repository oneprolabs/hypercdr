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
RUN chmod -R a+rX /usr/share/nginx/html
EXPOSE 80
