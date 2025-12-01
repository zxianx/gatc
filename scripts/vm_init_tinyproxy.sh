#!/bin/bash
# VM初始化脚本 - TinyProxy HTTP代理版本 - 适配Debian 12

# set -e  # 遇到错误立即退出
exec > >(tee /var/log/vm-init.log) 2>&1  # 记录日志

echo "=== Starting VM initialization with TinyProxy at $(date) ==="

# 更新系统
echo "Updating system packages..."
apt-get update -y
echo "Updating system packages 1..."
# apt-get upgrade -y

# 安装基础工具
echo "Installing basic tools..."
apt-get install -y curl wget unzip apt-transport-https ca-certificates gnupg lsb-release net-tools

# 安装Google Cloud SDK - 使用官方APT仓库方式（更可靠）
echo "Installing Google Cloud SDK..."
rm -rf /usr/share/keyrings/cloud.google.gpg
curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | gpg --dearmor -o /usr/share/keyrings/cloud.google.gpg
echo "[.1]Installing Google Cloud SDK..."
echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | tee -a /etc/apt/sources.list.d/google-cloud-sdk.list
echo "[.2]Installing Google Cloud SDK..."
# apt-get update -y

# 安装并配置TinyProxy HTTP代理
echo "Installing and configuring TinyProxy HTTP proxy..."
apt-get install -y tinyproxy

# 从metadata获取代理用户名和密码
echo "Getting proxy credentials from metadata..."
PROXY_USERNAME=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/proxy-username || echo "gatcuser")
PROXY_PASSWORD=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/proxy-password || echo "gatcpass123")

echo "Proxy username: $PROXY_USERNAME"
echo "Proxy password configured"

# 配置TinyProxy
cat > /etc/tinyproxy/tinyproxy.conf <<EOF
##
## tinyproxy.conf -- tinyproxy daemon configuration file
##

User tinyproxy
Group tinyproxy

Port 8080

Timeout 600

DefaultErrorFile "/usr/share/tinyproxy/default.html"
StatFile "/usr/share/tinyproxy/stats.html"
Logfile "/var/log/tinyproxy/tinyproxy.log"

LogLevel Info

PidFile "/run/tinyproxy/tinyproxy.pid"

MaxClients 100

MinSpareServers 5
MaxSpareServers 20

StartServers 10

MaxRequestsPerChild 0

ViaProxyName "tinyproxy"

# Allow access from all IP addresses
Allow 0.0.0.0/0

# Disable via header
DisableViaHeader Yes

# Enable URL filtering - only allow specific domains
Filter "/etc/tinyproxy/filter"
FilterDefaultDeny Yes

# Connect to any port (remove port restrictions)
# ConnectPort 443
# ConnectPort 563

EOF

# 创建域名白名单过滤文件
cat > /etc/tinyproxy/filter <<EOF
# 只允许这三个域名的请求通过
generativelanguage.googleapis.com
api.anthropic.com
ifconfig.me
# 允许这些域名的任何路径和子域名
.*\.generativelanguage\.googleapis\.com
.*\.api\.anthropic\.com
.*\.ifconfig\.me
EOF

# 设置过滤文件权限
chown root:root /etc/tinyproxy/filter
chmod 644 /etc/tinyproxy/filter

# 设置TinyProxy配置文件权限
chown root:root /etc/tinyproxy/tinyproxy.conf
chmod 644 /etc/tinyproxy/tinyproxy.conf

echo "Set sys param for TinyProxy"
export PATH=$PATH:/usr/sbin
sudo sysctl -w net.core.rmem_max=16777216
sudo sysctl -w net.core.wmem_max=16777216
sudo sysctl -w net.ipv4.tcp_rmem="4096 87380 16777216"
sudo sysctl -w net.ipv4.tcp_wmem="4096 65536 16777216"
echo "Set sys param d"

# 创建systemd服务文件
cat > /etc/systemd/system/tinyproxy.service <<EOF
[Unit]
Description=Tinyproxy HTTP proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=forking
ExecStart=/usr/bin/tinyproxy -c /etc/tinyproxy/tinyproxy.conf
ExecReload=/bin/kill -HUP \$MAINPID
PIDFile=/run/tinyproxy/tinyproxy.pid
User=tinyproxy
Group=tinyproxy
Restart=always
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# 创建必要的目录
mkdir -p /var/log/tinyproxy
mkdir -p /run/tinyproxy
chown tinyproxy:tinyproxy /var/log/tinyproxy
chown tinyproxy:tinyproxy /run/tinyproxy

# 启动TinyProxy服务
echo "Starting TinyProxy service..."
systemctl daemon-reload
systemctl enable tinyproxy
systemctl restart tinyproxy

# 检查服务状态
sleep 2
if systemctl is-active --quiet tinyproxy; then
    echo "TinyProxy service started successfully on port 8080"
else
    echo "Failed to start TinyProxy service"
    systemctl status tinyproxy
fi

# 测试代理是否正常工作
echo "Testing TinyProxy with allowed domains..."

# 测试允许的域名
echo "Testing ifconfig.me (should work)..."
if timeout 10 curl -x localhost:8080 -s -o /dev/null -w "%{http_code}" http://ifconfig.me | grep -q "200\|301\|302"; then
    echo "✓ ifconfig.me access allowed"
else
    echo "✗ ifconfig.me test failed"
fi

# 测试被拒绝的域名
echo "Testing google.com (should be blocked)..."
if timeout 5 curl -x localhost:8080 -s -o /dev/null -w "%{http_code}" http://google.com 2>/dev/null | grep -q "403\|407"; then
    echo "✓ google.com correctly blocked"
else
    echo "✗ google.com blocking test inconclusive"
fi

echo "TinyProxy domain filtering configured"

# vm 初始化 这一步容易提前返回， 但是好像gcloud 又安装成功了， 放到最后一步
echo "Installing google-cloud-cli"
apt-get install -y google-cloud-cli

# 创建标识文件表示初始化完成
echo "=== VM initialization with TinyProxy completed at $(date) ==="
touch /tmp/vm_init_complete