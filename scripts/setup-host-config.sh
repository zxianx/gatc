#!/bin/bash

# 宿主机配置目录初始化脚本
# 用于在宿主机上创建 /opt/gatc/conf 目录结构

set -e

GATC_CONF_DIR="/opt/gatc/conf"
GATC_DATA_DIR="/opt/gatc/mysql"

echo "Setting up GATC host configuration directories..."

# 创建配置目录
sudo mkdir -p "$GATC_CONF_DIR"/{gcp,dev}
sudo mkdir -p "$GATC_DATA_DIR"

# 设置目录权限
sudo chown -R $USER:$USER /opt/gatc
chmod 755 /opt/gatc
chmod 755 "$GATC_CONF_DIR"
chmod 700 "$GATC_CONF_DIR/gcp"  # 敏感配置目录

echo "Directory structure created:"
echo "  $GATC_CONF_DIR/"
echo "  ├── conf.yaml"
echo "  ├── resource.yaml"  
echo "  ├── dev/"
echo "  │   ├── conf.yaml"
echo "  │   └── resource.yaml"
echo "  └── gcp/"
echo "      ├── sa-key0.json"
echo "      ├── gatc_rsa"
echo "      └── gatc_rsa.pub"
echo ""
echo "  $GATC_DATA_DIR/ (for MySQL data)"

# 创建示例配置文件
if [ ! -f "$GATC_CONF_DIR/conf.yaml" ]; then
    cat > "$GATC_CONF_DIR/conf.yaml" << 'EOF'
# GATC 应用配置文件
port: 5401

log:
  level: "info"
  output: "stdout"
EOF
    echo "Created example conf.yaml"
fi

if [ ! -f "$GATC_CONF_DIR/resource.yaml" ]; then
    cat > "$GATC_CONF_DIR/resource.yaml" << 'EOF'
# GATC 资源配置文件
mysql:
  host: "mysql"
  port: 3306
  database: "gatc"
  username: "gatc_user"
  password: "gatc_password"
EOF
    echo "Created example resource.yaml"
fi

# 创建开发环境配置
if [ ! -f "$GATC_CONF_DIR/dev/conf.yaml" ]; then
    mkdir -p "$GATC_CONF_DIR/dev"
    cat > "$GATC_CONF_DIR/dev/conf.yaml" << 'EOF'
# GATC 开发环境配置
port: 5401

log:
  level: "debug"
  output: "stdout"
EOF
    echo "Created dev/conf.yaml"
fi

if [ ! -f "$GATC_CONF_DIR/dev/resource.yaml" ]; then
    cat > "$GATC_CONF_DIR/dev/resource.yaml" << 'EOF'
# GATC 开发环境资源配置
mysql:
  host: "localhost"
  port: 3306
  database: "gatc_dev"
  username: "root"
  password: "dev_password"
EOF
    echo "Created dev/resource.yaml"
fi

echo ""
echo "Configuration directories setup complete!"
echo ""
echo "Next steps:"
echo "1. Copy your GCP service account key to: $GATC_CONF_DIR/gcp/sa-key0.json"
echo "2. Generate SSH keys or copy existing ones to: $GATC_CONF_DIR/gcp/gatc_rsa*"
echo "3. Update configuration files as needed"
echo "4. Create .env file with database passwords"
echo "5. Run: docker-compose -f docker-compose.prod.yml up -d"