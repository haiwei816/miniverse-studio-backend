FROM node:24-alpine AS build
WORKDIR /web
COPY admin/web/package*.json ./
RUN npm ci
COPY admin/web/ ./
RUN npm run build
FROM nginx:1.29-alpine
COPY admin/web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /web/dist /usr/share/nginx/html
EXPOSE 8080
