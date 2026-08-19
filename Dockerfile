# syntax=docker/dockerfile:1

# 构建阶段
FROM golang:1.26 AS build
WORKDIR /src

# 先复制依赖清单再下载，利于层缓存复用。
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/bot ./cmd/bot

# 运行阶段
FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /out/bot /usr/local/bin/bot
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/bot"]
