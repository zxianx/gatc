#!/bin/bash
# VM初始化脚本 - 自定义HTTP代理服务器版本 - 适配Debian 12

# set -e  # 遇到错误立即退出
exec > >(tee /var/log/vm-init.log) 2>&1  # 记录日志

echo "=== Starting VM initialization with custom HTTP proxy at $(date) ==="

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

# 下载自定义HTTP代理服务器
echo "Installing custom HTTP proxy server..."
# TODO: 替换为实际的GitHub仓库URL
wget https://raw.githubusercontent.com/zxianx/gatc/refs/heads/main/vmHttpServerProxy/vm-http-proxy-linux-amd64 -O /usr/local/bin/vm-http-proxy || {
    echo "Failed to download vm-http-proxy, creating placeholder..."
    echo '#!/bin/bash' > /usr/local/bin/vm-http-proxy
    echo 'echo "vm-http-proxy placeholder"' >> /usr/local/bin/vm-http-proxy
}

chmod +x /usr/local/bin/vm-http-proxy

# 从metadata获取代理配置
echo "Getting proxy configuration from metadata..."
#PROXY_USERNAME=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/proxy-username || echo "gatcuser")
#PROXY_PASSWORD=$(curl -s -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/attributes/proxy-password || echo "gatcpass123")

echo "Proxy configuration loaded"

# 设置系统参数优化
echo "Setting system parameters for HTTP proxy"
export PATH=$PATH:/usr/sbin
sudo sysctl -w net.core.rmem_max=16777216
sudo sysctl -w net.core.wmem_max=16777216
sudo sysctl -w net.ipv4.tcp_rmem="4096 87380 16777216"
sudo sysctl -w net.ipv4.tcp_wmem="4096 65536 16777216"
echo "System parameters configured"

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

# 测试代理是否正常工作
echo "Testing custom HTTP proxy..."

# 测试健康检查端点
echo "Testing health endpoint..."
if timeout 5 curl -s http://localhost:1081/health | grep -q "OK"; then
    echo "✓ Health check passed"
else
    echo "✗ Health check failed"
fi

# 测试代理功能
echo "Testing proxy functionality with ifconfig.me..."
if timeout 10 curl -s "http://localhost:1081/px/http://ifconfig.me" | grep -E "^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$" > /dev/null; then
    echo "✓ Proxy functionality working"
else
    echo "✗ Proxy functionality test failed"
fi

# 测试HTTPS强制转换
echo "Testing HTTPS force conversion..."
if timeout 10 curl -s "http://localhost:1081/px/http://ifconfig.me" > /dev/null; then
    echo "✓ HTTP to HTTPS conversion test completed"
else
    echo "✗ HTTPS conversion test failed"
fi

echo "Custom HTTP proxy configuration completed"

# vm 初始化 这一步容易提前返回， 但是好像gcloud 又安装成功了， 放到最后一步
echo "Installing google-cloud-cli"
apt-get install -y google-cloud-cli

# 创建标识文件表示初始化完成
echo "=== VM initialization with custom HTTP proxy completed at $(date) ==="
touch /tmp/vm_init_complete