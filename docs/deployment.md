# 部署指南（systemd / docker）

## 环境变量

- `NEXTAI_HOST`（默认 `127.0.0.1`）
- `NEXTAI_PORT`（默认 `8088`）
- `NEXTAI_DATA_DIR`（默认 `.data`）
- `NEXTAI_API_KEY`（可选；设置后启用 API 鉴权）

## systemd 部署示例

1. 构建二进制

```bash
cd apps/gateway
go build -o /opt/nextai/bin/gateway ./cmd/gateway
```

2. 创建服务文件 `/etc/systemd/system/nextai-gateway.service`

```ini
[Unit]
Description=NextAI Gateway
After=network.target

[Service]
Type=simple
Environment=NEXTAI_HOST=0.0.0.0
Environment=NEXTAI_PORT=8088
Environment=NEXTAI_DATA_DIR=/var/lib/nextai
# Environment=NEXTAI_API_KEY=change-me
ExecStart=/opt/nextai/bin/gateway
Restart=always
RestartSec=3
User=www-data
Group=www-data

[Install]
WantedBy=multi-user.target
```

3. 启动服务

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now nextai-gateway
sudo systemctl status nextai-gateway
```

## Docker 部署示例

Dockerfile（最小示例）：

```dockerfile
FROM golang:1.22 AS build
WORKDIR /src
COPY . .
RUN cd apps/gateway && go build -o /out/gateway ./cmd/gateway

FROM debian:bookworm-slim
COPY --from=build /out/gateway /usr/local/bin/gateway
ENV NEXTAI_HOST=0.0.0.0
ENV NEXTAI_PORT=8088
ENV NEXTAI_DATA_DIR=/data
EXPOSE 8088
CMD ["gateway"]
```

运行：

```bash
docker run -d \
  --name nextai-gateway \
  -p 8088:8088 \
  -v nextai-data:/data \
  -e NEXTAI_API_KEY=change-me \
  nextai/gateway:latest
```
