# Maple Gateway 服务端多阶段构建。
# 数据平面(80/443) + Management API(4000) 同一二进制，由配置分端口监听。

# --- build stage ---
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/maple-gateway ./cmd/server

# --- runtime stage ---
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/maple-gateway /usr/local/bin/maple-gateway

EXPOSE 80 4000
# 容器内配置通过 MAPLE_* 环境变量注入（见 docker-compose.yml）
ENTRYPOINT ["maple-gateway"]
