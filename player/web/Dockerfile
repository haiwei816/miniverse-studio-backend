FROM node:24-alpine AS build
WORKDIR /src/player/web
COPY player/web/package*.json ./
RUN npm ci
COPY player/web/ ./
COPY scripts/build-cauldron.mjs /src/scripts/build-cauldron.mjs
RUN npm run build
FROM nginx:1.29-alpine
COPY player/web/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/player/web/dist /usr/share/nginx/html
EXPOSE 8080
