# 创建的vm支持自定义代理

type  httpProxyServer

#  自定中转httpProxyServer

## httpProxyServer 实现

本项目下 vmHttpServerProxy 目录建独立程序，尽量精简，不依赖配置文件  
配置通过env获取  
+ HttpServerProxyPort， 默认1081
+ force_https  默认true， 将http请求代理转为https取请求
+ proxy_url_keyword_white_list     "|"分隔的关键字
+ proxy_del_headers                "|"分隔的头名

核心逻辑，将 
/px/{url} 代理请求到  url， 注意host头的修改 ， 请求下游禁用长链接。

构建命令：
```shell
cd vmHttpServerProxy
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o vm-http-proxy-linux-amd64 .
```

## vm创建时添加自定义 httpProxyServer

采用个人git public仓库托管， 直接git上传到当前仓库

脚本片段
```shell
# 下载自定义HTTP代理服务器
echo "Installing custom HTTP proxy server..."
wget https://github.com/yourusername/gatc/releases/latest/download/vm-http-proxy -O /usr/local/bin/vm-http-proxy
chmod +x /usr/local/bin/vm-http-proxy

# 配置环境变量
export HttpServerProxyPort=1081
export force_https=false
export proxy_url_keyword_white_list="googleapis|anthropic|ifconfig.me|file.txt|gemini|generateContent|completions|chatgpt|openai|vertex|aiPlatform"
export proxy_del_headers="X-Forwarded-For"

# 创建systemd服务
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
Environment=proxy_url_keyword_white_list=generativelanguage.googleapis.com|api.anthropic.com|ifconfig.me
Environment=proxy_del_headers=User-Agent|X-Forwarded-For
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

# 启动自定义HTTP代理服务
systemctl daemon-reload
systemctl enable vm-http-proxy
systemctl start vm-http-proxy

# 验证服务状态
sleep 2
if systemctl is-active --quiet vm-http-proxy; then
    echo "✓ Custom HTTP proxy started on port 1081"
else
    echo "✗ Failed to start custom HTTP proxy"
    systemctl status vm-http-proxy
fi
```

## 完整需求理解

### 代理类型扩展
现有代理类型：
- `socks5`: dante-server SOCKS5代理 (端口1080)
- `tinyproxy`: TinyProxy HTTP代理 (端口8080)
- **新增** `httpProxyServer`: 自定义HTTP代理 (端口1081)

### 自定义代理特点
1. **路径代理**: 通过 `/px/{url}` 路径访问目标URL
2. **HTTPS强制**: 自动将HTTP请求转换为HTTPS
3. **域名白名单**: 只允许指定关键字的URL通过
4. **请求头过滤**: 删除指定的请求头
5. **禁用长连接**: 下游请求不使用keep-alive

### VM创建API修改
`CreateVMParam` 新增第三种代理类型：
```go
type CreateVMParam struct {
    Zone        string `json:"zone,omitempty"`
    MachineType string `json:"machine_type,omitempty"`
    Tag         string `json:"tag,omitempty"`
    ProxyType   string `json:"proxy_type,omitempty"` // socks5/tinyproxy/httpProxyServer
}
```

### 使用方式对比
```bash
# SOCKS5代理
curl --socks5 user:pass@vm-ip:1080 https://api.anthropic.com

# TinyProxy HTTP代理  
curl -x http://vm-ip:8080 https://api.anthropic.com

# 自定义HTTP代理服务器
curl http://vm-ip:1081/px/https://api.anthropic.com
```

### 实现步骤
1. ✅ 创建 `vmHttpServerProxy/` 目录和Go程序
2. ✅ 实现路径代理逻辑 `/px/{url}`
3. ✅ 添加域名白名单过滤
4. ✅ 添加请求头删除功能
5. ✅ 实现HTTPS强制转换
6. ✅ 修改VM初始化脚本支持第三种代理
7. ✅ 更新VM服务选择不同初始化脚本
8. ✅ 修改代理地址生成逻辑

这样理解需求正确吗？