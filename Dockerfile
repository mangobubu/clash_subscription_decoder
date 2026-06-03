# =========================================================
# 第一阶段 (Builder Stage)：全栈依赖拉取、前端编译与 Go 构建
# =========================================================
FROM golang:1.26-bookworm AS builder

# 针对中国大陆环境：替换 Debian 软件源为中国科学技术大学 (USTC) 镜像源以加速 apt-get
RUN sed -i 's/deb.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || true && \
    sed -i 's/security.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || true && \
    sed -i 's/deb.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list 2>/dev/null || true && \
    sed -i 's/security.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list 2>/dev/null || true

# 针对中国大陆环境：直接从官方 Node 镜像拷贝 Node.js 和 npm，完全规避 deb.nodesource.com 在国内连接超时的问题
COPY --from=node:20-bookworm /usr/local/bin /usr/local/bin
COPY --from=node:20-bookworm /usr/local/lib /usr/local/lib

# 针对中国大陆环境：配置 npm & pnpm 使用淘宝镜像源，极速拉取前端依赖
RUN npm config set registry https://registry.npmmirror.com && \
    npm install -g pnpm && \
    pnpm config set registry https://registry.npmmirror.com

# 针对中国大陆环境：配置 Go Proxy 代理，极速拉取 Go modules 依赖
ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /app

# 复制项目所有源文件以进行一键式打包
COPY . .

# 执行 Go 原生一键构建 (会自动探测 pnpm、自动装包、编译前端并生成 Linux 全栈二进制)
RUN go run build.go

# =========================================================
# 第二阶段 (Runner Stage)：极致轻量的 Debian 生产运行镜像
# =========================================================
FROM debian:bookworm-slim AS runner

# 针对中国大陆环境：替换 Debian 运行镜像的软件源为 USTC 镜像源，加速 tzdata 的安装
RUN sed -i 's/deb.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || true && \
    sed -i 's/security.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list.d/debian.sources 2>/dev/null || true && \
    sed -i 's/deb.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list 2>/dev/null || true && \
    sed -i 's/security.debian.org/mirrors.ustc.edu.cn/g' /etc/apt/sources.list 2>/dev/null || true

# 设置系统时区，确保容器内日志与系统时区完全同步
RUN apt-get update && \
    apt-get install -y --no-install-recommends tzdata && \
    ln -sf /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# 从构建阶段拷贝编译好的包含全量前端的独立全栈二进制程序
COPY --from=builder /app/release/ClashSubAST /app/ClashSubAST

# 服务默认监听端口，可被运行环境中的 SERVER_PORT 覆盖
ENV SERVER_PORT=8080

# 暴露后端监听端口
EXPOSE 8080

# 启动全栈应用 (运行目录中需挂载或提供 config.toml)
CMD ["/app/ClashSubAST"]
