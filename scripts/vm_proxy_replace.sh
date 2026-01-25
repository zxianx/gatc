#!/bin/bash

# chmod +x vm_proxy_replace.sh
# 22 17 * * * /bin/bash /home/user1/cron/vm_proxy_replace.sh

# 配置变量
URL="http://38.129.137.18:8401/api/v1/vm/replace-proxy-resource"
TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')
TAG_DATE=$(date '+%y%m%d%H%M')
LOG_FILE="/home/user1/cron/vm_proxy_replace.log"

# 创建日志目录（如果不存在）
mkdir -p $(dirname "$LOG_FILE")

# 记录开始时间
echo "[$TIMESTAMP] 开始执行VM代理资源替换请求" >> "$LOG_FILE"

# 执行curl请求并捕获完整响应
response=$(curl -s -w "\n%{http_code}\n%{exitcode}" \
  --location --request POST "$URL" \
  --header 'User-Agent: Apifox/1.0.0 (https://apifox.com)' \
  --header 'Content-Type: application/json' \
  --connect-timeout 30 \
  --max-time 600 \
  --data-raw '{
    "num": 50,
    "tag": "replace'$TAG_DATE'",
    "proxy_type": "server"
  }')

# 解析响应
http_body=$(echo "$response" | head -n -2)
http_code=$(echo "$response" | tail -n 2 | head -n 1)
curl_exit_code=$(echo "$response" | tail -n 1)

# 记录结果到日志文件
echo "=== 请求详情 ===" >> "$LOG_FILE"
echo "时间: $TIMESTAMP" >> "$LOG_FILE"
echo "URL: $URL" >> "$LOG_FILE"
echo "Tag: replace$TAG_DATE" >> "$LOG_FILE"
echo "CURL退出码: $curl_exit_code" >> "$LOG_FILE"
echo "HTTP状态码: $http_code" >> "$LOG_FILE"
echo "响应内容: $http_body" >> "$LOG_FILE"

# 控制台输出
echo "=== VM代理资源替换执行结果 ==="
echo "执行时间: $TIMESTAMP"
echo "Tag: replace$TAG_DATE"
echo "CURL退出码: $curl_exit_code"
echo "HTTP状态码: $http_code"
echo "响应内容: $http_body"

# 根据结果判断执行状态
if [ $curl_exit_code -eq 0 ]; then
    if [ $http_code -eq 200 ]; then
        echo "状态: 请求成功" | tee -a "$LOG_FILE"
        exit 0
    else
        echo "状态: HTTP请求失败，状态码: $http_code" | tee -a "$LOG_FILE"
        exit 1
    fi
else
    echo "状态: CURL请求失败，错误码: $curl_exit_code" | tee -a "$LOG_FILE"
    exit 2
fi