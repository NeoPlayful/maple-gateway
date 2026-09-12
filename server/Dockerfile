# Maple Gateway 服务端多阶段构建。
# 三个二进制共用同一镜像：Gateway(数据平面 + Management API)、Container Manager、Node Agent。
# 各进程由 compose 覆盖 entrypoint 选择；配置经 MAPLE_* 环境变量注入。

# --- build stage ---
FROM golang:1.26-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/maple-gateway ./cmd/server \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/maple-cm ./cmd/containermanager \
 && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/maple-nodeagent ./cmd/nodeagent

# --- runtime stage ---
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/maple-gateway /usr/local/bin/maple-gateway
COPY --from=builder /out/maple-cm /usr/local/bin/maple-cm
COPY --from=builder /out/maple-nodeagent /usr/local/bin/maple-nodeagent

# Gateway 数据平面(80/443) + Management API(8090)；CM(:9091)；Agent(:9092)
EXPOSE 80 443 8090 9091 9092
# 容器内配置通过 MAPLE_* 环境变量注入（见 docker-compose.yml）
ENTRYPOINT ["maple-gateway"]
