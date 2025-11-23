# 多阶段构建
FROM golang:1.22-alpine AS builder

# 设置工作目录
WORKDIR /app

# 复制go mod文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o gatc .

# 运行时镜像 - 使用官方gcloud镜像
FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:latest

# 安装必要的系统包
RUN apt-get update && apt-get install -y \
    curl \
    openssh-client \
    net-tools \
    && rm -rf /var/lib/apt/lists/*

# 创建非root用户
RUN useradd -m -s /bin/bash gatc

# 设置工作目录
WORKDIR /app

# 从builder阶段复制应用程序
COPY --from=builder /app/gatc .

# 仅复制scripts目录（不包含敏感配置）
COPY --chown=gatc:gatc scripts/ ./scripts/

# 确保脚本有执行权限
RUN chmod +x ./scripts/*.sh

# 创建conf目录（用于挂载）
RUN mkdir -p ./conf && chown -R gatc:gatc ./conf

# 切换到非root用户
USER gatc

# 暴露端口
EXPOSE 5401

# 健康检查 - 使用专门的健康检查路由
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:5401/health || exit 1

# 启动命令
CMD ["./gatc"]