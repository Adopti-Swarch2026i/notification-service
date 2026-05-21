# Stage 1: Build
# golang:1.25-alpine porque gin@v1.12+ exige Go 1.25 mínimo.
FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# go mod tidy regenera go.mod/go.sum según los imports actuales (necesario
# tras añadir la importación directa de firebase.google.com/go/v4/auth en
# el router para A7).
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o notification-service ./cmd/server/main.go

# Stage 2: Runtime
FROM alpine:3.19

# ca-certificates es obligatorio para que las llamadas TLS a SendGrid y FCM
# funcionen desde un contenedor alpine mínimo.
RUN apk add --no-cache ca-certificates curl

WORKDIR /app
COPY --from=builder /app/notification-service .
COPY migrations/ ./migrations/

EXPOSE 8082

CMD ["./notification-service"]
