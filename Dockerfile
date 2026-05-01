# Stage 1: Build
FROM golang:alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o notification-service ./cmd/server/main.go

# Stage 2: Runtime
FROM alpine:3.19

WORKDIR /app
# We need to copy the binary and the database migrations that the app uses on startup.
COPY --from=builder /app/notification-service .
COPY migrations/ ./migrations/

EXPOSE 8082

CMD ["./notification-service"]
