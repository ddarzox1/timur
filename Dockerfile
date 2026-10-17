# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
# Шаблоны, статика и миграции встроены через embed — в образ идёт только бинарник.
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /qauipsiz ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -u 10001 app \
 && mkdir -p /app/uploads && chown app:app /app/uploads
WORKDIR /app
COPY --from=build /qauipsiz /app/qauipsiz
USER app
ENV ADDR=:8080 UPLOADS_DIR=/app/uploads
EXPOSE 8080
VOLUME /app/uploads
ENTRYPOINT ["/app/qauipsiz"]
