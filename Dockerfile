# Многостадийная сборка для Pweper Bot
# Основано на Dockerfile от bothost.ru

# ── ЭТАП 1: Сборка ──────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git ca-certificates tzdata

# Копируем исходный код
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# Сборка статического бинарника (CGO_ENABLED=0) для универсальной совместимости
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-s -w" -o pweper-bot ./cmd/bot

# ── ЭТАП 2: Финальный образ ─────────────────────────────────────────────────
FROM alpine:latest

WORKDIR /app

# Устанавливаем ca-certificates для HTTPS и tzdata для timezone
# sqlite не нужен (мы используем pure-Go modernc.org/sqlite)
RUN apk --no-cache add ca-certificates tzdata wget unzip

# Директория для постоянных данных: БД, файлы состояния, логи.
# Монтируется как Docker volume — данные сохраняются при перезапуске.
ENV DATA_DIR=/app/data
RUN mkdir -p /app/data /app/logs /app/work /app/bin && chmod 777 /app/data

# Копируем скомпилированный бинарник
COPY --from=builder /build/pweper-bot /usr/local/bin/pweper-bot
RUN chmod +x /usr/local/bin/pweper-bot

# Копируем ассеты (шаблоны для команд)
COPY assets/ /app/assets/

# astcenc скачается автоматически при первом использовании BTX
# (встроенная функция в боте, работает без прав root)

# Environment defaults (MUST be set via bothost panel)
ENV TOKEN="" \
    API_ID="" \
    API_HASH="" \
    ADMIN_IDS="" \
    ONLYSQ_API_KEY="" \
    DB_PATH=/app/data/users.db \
    WORK_DIR=/app/work \
    ASSETS_DIR=/app/assets \
    LOG_FILE=/app/logs/pweper.log \
    LOG_LEVEL=info \
    ENABLE_MTPROTO=0

# Entrypoint script для правильных прав
RUN echo '#!/bin/sh' > /usr/local/bin/entrypoint.sh && \
    echo 'set -e' >> /usr/local/bin/entrypoint.sh && \
    echo 'mkdir -p /app/data /app/logs /app/work /app/bin' >> /usr/local/bin/entrypoint.sh && \
    echo 'chmod 777 /app/data /app/logs /app/work /app/bin' >> /usr/local/bin/entrypoint.sh && \
    echo 'exec "$@"' >> /usr/local/bin/entrypoint.sh && \
    chmod +x /usr/local/bin/entrypoint.sh

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

# Запуск бота
CMD ["pweper-bot"]
