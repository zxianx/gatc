# VM HTTP Proxy Server

## 概述

`vmHttpServerProxy` 是GATC项目内嵌的独立HTTP代理服务器，专门设计用于在GCP虚拟机中运行，为大模型API请求提供HTTP代理功能。

## 代码风格
 
 尽量精简，配置直接在包内 const 定义，单例组件直接包级别定义，main init， 不考虑安全问题，无需过多异常判断。

## 功能特性

### 🚀 核心功能
- **HTTP转发代理**：通过`/px/{url}`路径转发HTTP/HTTPS请求
- **强制HTTPS**：自动将HTTP请求升级为HTTPS（可配置）
- **URL白名单过滤**：基于关键词的URL访问控制
- **请求头管理**：支持删除指定的请求头


通过环境变量进行配置：

| 环境变量 | 默认值   | 说明 |
|---------|-------|------|
| `HttpServerProxyPort` | 1081  | 服务器监听端口 |
| `force_https` | false | 是否强制将HTTP请求升级为HTTPS |
| `proxy_url_keyword_white_list` | 空     | URL白名单关键词，用`\|`分隔 |
| `proxy_del_headers` | 空     | 需要删除的请求头，用`\|`分隔 |  

（注意上面未按标准env的大写方式， 变量名参照表格）  


## 支持Gemini异步批处理

将请求聚合后提交给Gemini批处理API，等待异步处理完成后返回结果。

**触发条件:**
- 请求头包含 `X-Gemini-Batch: 1`
- 目标URL包含 `v1beta/models/gemini`

**批处理流程:**
1. 收集请求（2分钟内最多20个）
2. 统一提交到 `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:batchGenerateContent`
3. 轮询作业状态（10秒间隔）: PENDING → RUNNING → SUCCEEDED
4. 提取结果并按key分发回各个原始请求

**超时配置:**
- 提交超时: 30秒（HTTP客户端超时）
- 轮询超时: 2分钟 + 请求数 × 30秒
- 轮询间隔: 10秒

**注意:** 
- 请求体需符合Gemini API格式（`contents`字段）
- 批处理方式参考 gemini-batch-test 目录


# 构建命令
```shell
cd vmHttpServerProxy
GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o vm-http-proxy-linux-amd64 .
```