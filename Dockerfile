# ---- 构建阶段 ----
FROM golang:1.23-alpine AS builder

WORKDIR /src

# 项目零第三方依赖；单独拷贝 go.mod 便于依赖变化时复用构建缓存
COPY go.mod ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/memos-wechat .

# ---- 运行阶段 ----
FROM alpine:3.20

# 调用 Memos 走 HTTPS，运行镜像必须自带根证书
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/memos-wechat /usr/local/bin/memos-wechat

USER nobody
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/memos-wechat"]
