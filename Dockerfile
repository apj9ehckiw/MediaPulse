# haijiao-web 多阶段构建
# 前端 React+Vite -> Go embed -> 运行镜像仅 alpine+ffmpeg，~50MB

# ---------- 阶段 1: 前端构建 ----------
FROM node:20-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci || npm install
COPY frontend/ ./
# 显式输出到 /dist（vite.config.ts 里 outDir 指向 ../backend/web/dist，
# 在容器内不存在该相对路径，用 CLI 参数覆盖）
RUN npm run build -- --outDir /dist --emptyOutDir

# ---------- 阶段 2: Go 构建 ----------
FROM golang:1.23-alpine AS backend
WORKDIR /src
# 先放一个占位 index.html 保证 go:embed 可编译，再用真实产物覆盖
RUN mkdir -p backend/web/dist && echo placeholder > backend/web/dist/index.html
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /dist/ backend/web/dist/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/haijiao-web ./backend/cmd/server

# ---------- 阶段 3: 运行镜像 ----------
FROM alpine:3.20
RUN apk add --no-cache ffmpeg ca-certificates tzdata
WORKDIR /data
COPY --from=backend /out/haijiao-web /usr/local/bin/haijiao-web

ENV HJ_ADDR=":8080" \
    HJ_DATA_DIR="/data" \
    HJ_INTERVAL="600" \
    TZ="Asia/Shanghai"

VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["haijiao-web"]
