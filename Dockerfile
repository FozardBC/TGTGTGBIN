
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Копируем модульные файлы и зависимости
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходный код
COPY . .

# Собираем бинарник
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o vkid_bot .

# Финальный образ
FROM alpine:latest

RUN apk --no-cache add tzdata ca-certificates && \
    update-ca-certificates

WORKDIR /root

COPY --from=builder /app/vkid_bot .
RUN touch vkids.json

CMD ["./vkid_bot"]