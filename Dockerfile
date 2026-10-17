FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Шаблоны, статика и миграции встроены через embed — в образ идёт только бинарник.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /qauipsiz ./cmd/server

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
