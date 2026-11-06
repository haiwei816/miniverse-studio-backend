FROM golang:1.27.3 AS build
WORKDIR /src
COPY backend/ .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build go mod download && CGO_ENABLED=0 go build -o /api ./cmd/api && CGO_ENABLED=0 go build -o /migrate ./cmd/migrate
FROM alpine:3.23
RUN adduser -D app
WORKDIR /app
COPY --from=build /api /migrate ./
COPY backend/migrations ./migrations
USER app
ENTRYPOINT ["/app/api"]
