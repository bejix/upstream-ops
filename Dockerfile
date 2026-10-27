# 一体化镜像：前端 build → 嵌入 Go 二进制 → alpine 运行时
#
# 三阶段构建：
#   1) frontend-builder — node + pnpm 跑 `vite build`，产物在 /web/dist
#   2) go-builder       — 把 dist 拷到 web/dist，go build 通过 //go:embed 嵌入二进制
#   3) runtime          — 极小 alpine 镜像只放一个静态二进制
#
# 由于第二阶段需要 frontend 产物，构建 context 必须是 repo 根目录：
#   docker build -t upstream-ops:dev .
#   或在 docker-compose 里写 context: .

# ---------- Stage 1: 前端 ----------
# 前端产物是纯静态资源，与目标架构无关。固定用构建机原生架构执行，
# 否则 buildx 会在 QEMU 模拟的 arm64 下把 pnpm install / vite build 再跑一遍，
# 单这一步就要多花两三分钟。
FROM --platform=$BUILDPLATFORM node:20-alpine AS frontend-builder
WORKDIR /web

# pnpm-workspace.yaml 用了 allowBuilds: 这种 pnpm 10.4+ 才支持的字段，
# corepack 默认的 pnpm shim 版本可能太旧，显式 pin 一个已知兼容版本。
RUN corepack enable && corepack prepare pnpm@10.4.0 --activate

# 先拷依赖清单走缓存层
COPY frontend/package.json frontend/pnpm-lock.yaml frontend/pnpm-workspace.yaml ./
# 不用 --frozen-lockfile：lockfile 不严格匹配时只警告不报错；
# 在 CI 里如果发现 lockfile 已经稳定可信，可以改回 --frozen-lockfile 锁定可复现性。
RUN pnpm install --no-frozen-lockfile

# 再拷源码，build 产物在 /web/dist
COPY frontend/ ./
RUN pnpm build

# ---------- Stage 2: 后端 ----------
# Go 原生支持交叉编译，同样不需要 QEMU：在构建机原生架构上按 TARGETARCH 出目标平台二进制。
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS go-builder
ARG TARGETOS=linux
ARG TARGETARCH
WORKDIR /src

# 先 go.mod / go.sum 走缓存
COPY go.mod go.sum ./
RUN go mod download

# 然后整份源码
COPY . ./

# 关键：把前端 dist 替换掉占位的 web/dist，让 //go:embed 抓到真东西
RUN rm -rf ./web/dist
COPY --from=frontend-builder /web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/upstream-ops \
        ./cmd/server

# ---------- Stage 3: 运行时 ----------
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata wget && \
    mkdir -p /app/data
WORKDIR /app
COPY --from=go-builder /out/upstream-ops /usr/local/bin/upstream-ops
EXPOSE 8418
ENTRYPOINT ["upstream-ops"]
