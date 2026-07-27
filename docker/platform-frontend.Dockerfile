ARG NGINX_IMAGE=nginx:1.27-alpine

FROM ${NGINX_IMAGE}

COPY nginx/default.conf /etc/nginx/conf.d/default.conf
COPY dist/ /usr/share/nginx/html/

EXPOSE 3002
