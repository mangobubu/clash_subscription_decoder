# =========================================================
# 第一阶段 (Builder Stage)：全栈依赖拉取、前端编译与 Go 构建
# =========================================================
FROM golang:1.20-bookworm AS builder

# 安装 Node.js 18.x 和全局 pnpm 包管理器
RUN curl -fsSL https://deb.nodesource.com/setup_18.x | bash - && \
    apt-get install -y nodejs && \
    npm install -g pnpm

WORKDIR /app

# 复制项目所有源文件以进行一键式打包
COPY . .

# 执行 Go 原生一键构建 (会自动探测 pnpm、自动装包、编译前端并生成 Linux 全栈二进制)
RUN go run build.go

# =========================================================
# 第二阶段 (Runner Stage)：极致轻量的 Debian 生产运行镜像
# =========================================================
FROM debian:bookworm-slim AS runner

# 设置系统时区，确保容器内日志与系统时区完全同步
RUN apt-get update && \
    apt-get install -y --no-install-recommends tzdata && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# 从构建阶段拷贝编译好的包含全量前端的独立全栈二进制程序
COPY --from=builder /app/release/clash-proxy /app/clash-proxy

# 暴露后端监听端口
EXPOSE 8080

# 启动全栈应用 (运行目录中需挂载或提供 config.toml)
CMD ["/app/clash-proxy"]
