# 构建阶段：编译常驻计费服务端 cmd/server
FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/server ./cmd/server

# 运行阶段：最小 alpine，非 root 运行
FROM alpine:3.21
RUN adduser -D -u 10001 app && mkdir -p /etc/desubber /data/src /data/result && chown app /data/src /data/result
COPY --from=build /out/server /usr/local/bin/server
# 配置文件路径：默认 /etc/desubber/config.yaml，可用环境变量 CONFIG 覆盖
ENV CONFIG=/etc/desubber/config.yaml
EXPOSE 18080
USER app
ENTRYPOINT ["sh", "-c", "exec /usr/local/bin/server \"$CONFIG\""]
