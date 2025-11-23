#!/bin/bash
# VM初始化脚本 - 适配Debian 12

set -e  # 遇到错误立即退出
exec > >(tee /var/log/vm-init.log) 2>&1  # 记录日志

echo "=== Starting VM initialization at $(date) ==="

# 更新系统
echo "Updating system packages..."
apt-get update -y
apt-get upgrade -y

# 安装基础工具
echo "Installing basic tools..."
apt-get install -y curl wget unzip apt-transport-https ca-certificates gnupg lsb-release net-tools

# 安装Google Cloud SDK - 使用官方APT仓库方式（更可靠）
echo "Installing Google Cloud SDK..."
curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg
echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | tee -a /etc/apt/sources.list.d/google-cloud-sdk.list
apt-get update -y
apt-get install -y google-cloud-cli

# 安装并配置SOCKS5代理 (dante-server)
echo "Installing and configuring SOCKS5 proxy..."
apt-get install -y dante-server

# 获取网络接口名称（Debian 12可能不是eth0）
INTERFACE=$(ip route | grep default | awk '{print $5}' | head -n1)
echo "Using network interface: $INTERFACE"

sysctl -w net.core.rmem_max=16777216
sysctl -w net.core.wmem_max=16777216
sysctl -w net.ipv4.tcp_rmem="4096 87380 16777216"
sysctl -w net.ipv4.tcp_wmem="4096 65536 16777216"

cat > /etc/danted.conf <<EOF
logoutput: /dev/null
internal: 0.0.0.0 port = 1080
external: $INTERFACE
clientmethod: none
socksmethod: none
user.privileged: root
user.unprivileged: nobody
sockbufsize: 65536
maxclients: 100

client pass {
    from: 0.0.0.0/0 to: 0.0.0.0/0
    log: error
}

socks pass {
    from: 0.0.0.0/0 to: 0.0.0.0/0
    log: error
}
EOF

# 启动SOCKS5代理服务
echo "Starting SOCKS5 proxy service..."
systemctl enable danted
systemctl restart danted

# 检查服务状态
sleep 2
if systemctl is-active --quiet danted; then
    echo "SOCKS5 proxy service started successfully"
else
    echo "Failed to start SOCKS5 proxy service"
    systemctl status danted
fi

# 检查端口是否监听
if netstat -ln | grep -q ":1080"; then
    echo "SOCKS5 proxy is listening on port 1080"
else
    echo "Warning: SOCKS5 proxy port 1080 not found"
fi

# 创建标识文件表示初始化完成
echo "=== VM initialization completed at $(date) ==="
touch /tmp/vm_init_complete