#!/bin/bash
# VM初始化脚本 - 同时安装三种代理（SOCKS5 + TinyProxy + HTTP Proxy Server）- 适配Debian 12

# set -e  # 遇到错误立即退出
exec > >(tee /var/log/vm-init.log) 2>&1  # 记录日志

echo "=== Starting VM initialization with ALL proxy types at $(date) ==="

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

# 从metadata获取代理用户名和密码
echo "Getting proxy credentials from metadata..."
PROXY_USERNAME=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/proxy-username || echo "gatcuser")
PROXY_PASSWORD=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/proxy-password || echo "gatcpass123")

echo "Proxy username: $PROXY_USERNAME"
echo "Proxy password configured"

# 设置系统参数优化
echo "Setting system parameters for proxies"
export PATH=$PATH:/usr/sbin
sudo sysctl -w net.core.rmem_max=16777216
sudo sysctl -w net.core.wmem_max=16777216
sudo sysctl -w net.ipv4.tcp_rmem="4096 87380 16777216"
sudo sysctl -w net.ipv4.tcp_wmem="4096 65536 16777216"
echo "System parameters configured"

# ============================================
# 1. 安装并配置SOCKS5代理 (dante-server) - 端口 1080
# ============================================
echo "=== Installing and configuring SOCKS5 proxy (port 1080) ==="
apt-get install -y dante-server

# 获取网络接口名称（Debian 12可能不是eth0）
INTERFACE=$(ip route | grep default | awk '{print $5}' | head -n1)
echo "Using network interface: $INTERFACE"

# 创建系统用户用于SOCKS5认证
useradd -M -r -s /usr/sbin/nologin "$PROXY_USERNAME" 2>/dev/null || true
echo "$PROXY_USERNAME:$PROXY_PASSWORD" | chpasswd || true

# clientmethod: none  不需要让客户端连接时就做 PAM 认证
cat > /etc/danted.conf <<EOF
logoutput: /var/log/danted.log
internal: 0.0.0.0 port = 1080
external: $INTERFACE
clientmethod: none
socksmethod: username
user.privileged: root
user.unprivileged: nobody

client pass {
    from: 0.0.0.0/0 to: 0.0.0.0/0
    log: error
}

socks pass {
    from: 0.0.0.0/0 to: 0.0.0.0/0
    log: error
}
EOF

cat > /etc/systemd/system/danted.service <<EOF
[Unit]
Description=SOCKS5 Proxy (Dante)
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/sbin/danted -f /etc/danted.conf
Restart=always
RestartSec=5
User=root
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# 启动SOCKS5代理服务
echo "Starting SOCKS5 proxy service..."
systemctl daemon-reload
systemctl enable danted
systemctl restart danted

# 检查服务状态
sleep 1
if systemctl is-active --quiet danted; then
    echo "✓ SOCKS5 proxy service started successfully on port 1080"
else
    echo "✗ Failed to start SOCKS5 proxy service"
    systemctl status danted
fi

# ============================================
# 2. 安装并配置TinyProxy HTTP代理 - 端口 8080
# ============================================
echo "=== Installing and configuring TinyProxy HTTP proxy (port 8080) ==="
apt-get install -y tinyproxy

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
    echo "✓ TinyProxy service started successfully on port 8080"
else
    echo "✗ Failed to start TinyProxy service"
    systemctl status tinyproxy
fi

# ============================================
# 3. 安装并配置自定义HTTP代理服务器 - 端口 1081
# ============================================
echo "=== Installing custom HTTP proxy server (port 1081) ==="

# 下载自定义HTTP代理服务器
wget https://raw.githubusercontent.com/zxianx/gatc/refs/heads/main/vmHttpServerProxy/vm-http-proxy-linux-amd64 -O /usr/local/bin/vm-http-proxy || {
    echo "Failed to download vm-http-proxy, creating placeholder..."
    echo '#!/bin/bash' > /usr/local/bin/vm-http-proxy
    echo 'echo "vm-http-proxy placeholder"' >> /usr/local/bin/vm-http-proxy
}

chmod +x /usr/local/bin/vm-http-proxy

# 创建systemd服务文件
cat > /etc/systemd/system/vm-http-proxy.service <<EOF
[Unit]
Description=Custom HTTP Proxy Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/vm-http-proxy
Restart=always
RestartSec=5
User=root
Environment=HttpServerProxyPort=1081
Environment=force_https=false
Environment=proxy_url_keyword_white_list=googleapis|anthropic|ifconfig|gemini|generateContent|completions|chatgpt|openai|vertex|aiPlatform
Environment=proxy_del_headers=User-Agent|X-Forwarded-For
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# 启动自定义HTTP代理服务
echo "Starting custom HTTP proxy service..."
systemctl daemon-reload
systemctl enable vm-http-proxy
systemctl start vm-http-proxy

# 检查服务状态
sleep 3
if systemctl is-active --quiet vm-http-proxy; then
    echo "✓ Custom HTTP proxy started successfully on port 1081"
else
    echo "✗ Failed to start custom HTTP proxy service"
    systemctl status vm-http-proxy
    echo "Service logs:"
    journalctl -u vm-http-proxy -n 20 --no-pager
fi

# ============================================
# 测试所有代理
# ============================================
echo "=== Testing all proxy services ==="

# 测试自定义HTTP代理
echo "Testing custom HTTP proxy (port 1081)..."
if timeout 10 curl -s "http://localhost:1081/px/http://ifconfig.me" | grep -E "^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$" > /dev/null; then
    echo "✓ Custom HTTP proxy (1081) working"
else
    echo "✗ Custom HTTP proxy (1081) test failed"
fi

# 测试TinyProxy
echo "Testing TinyProxy (port 8080)..."
if timeout 10 curl -x localhost:8080 -s -o /dev/null -w "%{http_code}" http://ifconfig.me | grep -q "200\|301\|302"; then
    echo "✓ TinyProxy (8080) working"
else
    echo "✗ TinyProxy (8080) test failed"
fi

echo "All proxy services configured"

# vm 初始化 这一步容易提前返回， 但是好像gcloud 又安装成功了， 放到最后一步
echo "Installing google-cloud-cli"
apt-get install -y google-cloud-cli

# 创建标识文件表示初始化完成
echo "=== VM initialization with ALL proxy types completed at $(date) ==="
touch /tmp/vm_init_complete
