# 📌 影响范围：读取 Go 与 WebUI 依赖，生成包含 Vue 静态资源的机器人 Linux 容器镜像。
FROM node:22-alpine AS web-builder

WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web ./
RUN npm run build

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
COPY --from=web-builder /src/dist /app/web/dist
USER bot
EXPOSE 18800
ENTRYPOINT ["/app/bot"]
