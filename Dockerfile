# 📌 影响范围：读取 Go 模块依赖并生成机器人 Linux 容器镜像。
FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY pkg ./pkg
COPY plugins ./plugins
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/bot ./cmd/bot
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S bot \
    && adduser -S -G bot bot
WORKDIR /app
COPY --from=builder /out/bot /app/bot
COPY --from=builder /out/migrate /app/migrate
USER bot
EXPOSE 18800
ENTRYPOINT ["/app/bot"]
