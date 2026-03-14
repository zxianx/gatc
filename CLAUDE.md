# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

GATC (Google Account Token Collector) 是一个基于 Go 的自动化系统，用于管理 Google Cloud Platform (GCP) 账户和虚拟机以获取 Gemini API Token。系统使用"白账户"（稳定的 GCP 账户）创建 VM，并通过自动化注册和项目创建工作流管理多个用户账户。

## 架构

### 分层架构

项目采用经典的分层架构：

```
HTTP API (handler/)
    ↓
Service Layer (service/)
    ↓
Data Access Layer (dao/)
    ↓
Database (MySQL)
```

### 核心组件

**Service 层：**
- `service/vm_service.go`: VM 生命周期管理（创建、删除、状态同步、定时清理）
- `service/gcp_account_service.go`: GCP 账户注册流程管理，协调 gcloud 子包完成认证
- `service/project_service.go`: GCP 项目创建、账单绑定、API Token 获取
- `service/gcloud/`: gcloud CLI 自动化核心包
  - `login_session.go`: 基于 SSH 的交互式 gcloud auth login 会话管理
  - `account_auth_login.go`: 账户认证流程
  - `project_process.go`: 项目创建和处理流程
  - `post_login_process_v2.go`: 登录后的项目配置和 Token 获取

**DAO 层：**
- `dao/gcp_account.go`: GCP 账户数据模型，支持 email+project_id 复合唯一索引，实现多项目 per 账户
- `dao/vm_instance.go`: VM 实例数据模型，管理 VM 生命周期和代理配置

**基础设施：**
- `base/zlog/`: 基于 zap 的结构化日志，支持请求 ID 追踪
- `base/config/`: GCP 配置管理（SSH 密钥、白账户项目 ID）
- `base/ratelimit/`: 基于邮箱的请求频率限制（10 分钟滑动窗口）
- `cron/`: 基于 robfig/cron 的定时任务管理

### 关键数据模型

**GCPAccount** (`dao/gcp_account.go`):
- 账单状态：`BillingStatusUnbound`(0), `BillingStatusBound`(1), `BillingStatusDetach`(2)
- Token 状态：`TokenStatusNone`(0), `TokenStatusCreateFail`(1), `TokenStatusGot`(2), `TokenStatusInvalid`(3)
- 认证状态：`AuthStatusNotLogin`(0), `AuthStatusLoggedIn`(1), `AuthStatusLoginFailed`(2), `AuthStatusVMError`(3)
- 复合唯一索引：email + project_id，支持一个邮箱多个项目

**VMInstance** (`dao/vm_instance.go`):
- 状态：`VMStatusRunning`(1), `VMStatusStopped`(2), `VMStatusDeleted`(3), `VMStatusPendingDelete`(4)
- 代理类型：socks5(默认), tinyproxy, httpProxyServer
- SSH 连接信息：外部 IP、内部 IP、SSH 用户名、密钥内容

### 单例模式

项目广泛使用包级全局单例：

```go
// service 层
var GVmService = &VMService{}
var GGcpAccountService = &GcpAccountService{}
var GProjectService = &ProjectService{}

// dao 层
var GGcpAccountDao = &GcpAccountDao{}
var GVmInstanceDao = &VMInstanceDao{}

// gcloud 包
var GAuthSessionSessionCache = &AuthSessionSessionCache{}
```

### WorkCtx 工作上下文

`service/gcloud/common.go` 中定义的 WorkCtx 贯穿整个认证流程：

```go
type WorkCtx struct {
    SessionID  string          // 会话 ID，用于追踪
    Email      string          // 当前处理的邮箱
    VMInstance *dao.VMInstance // 关联的 VM 实例
    GinCtx     *gin.Context    // HTTP 上下文
}
```

### VM 智能复用策略

- `ForceCreateVm` 参数控制 VM 创建策略（默认 false，启用智能复用）
- VM 验证包括数据库状态和 GCP 实际存在性双重校验（通过 gcloud CLI）
- 自动清理无效的 VM 关联，同步账户数据
- 新 VM 通过初始化脚本安装 gcloud CLI 和 SOCKS5 代理

### 认证会话状态机

`service/gcloud/login_session.go` 实现自动化 gcloud 认证：

```go
type AuthStatus int
const (
    AuthSessionStatusNone       AuthStatus = 0
    AuthSessionStatusBeginLogin AuthStatus = 2
    AuthSessionStatusWaitKey    AuthStatus = 3  // 等待用户输入授权码
    AuthSessionStatusGetKey     AuthStatus = 4  // 已获取授权码
    AuthSessionStatusDone       AuthStatus = 10 // 认证完成
    AuthSessionStatusFail       AuthStatus = 11 // 认证失败
)
```

流程：SSH 到 VM → 执行 `gcloud auth login --no-launch-browser` → 解析输出获取授权 URL → 用户完成授权后提交授权码 → 完成认证。

## 开发命令

### 本地开发

```bash
# 直接运行（需要本地配置文件）
go run .

# 构建二进制
go build -o gatc .

# 依赖管理
go mod tidy

# 运行测试
go test ./...

# 运行单个测试
go test -v ./base/ratelimit/...
```

### Docker 构建与部署

```bash
# 构建镜像
docker build -t gatc .

# 生产部署（配置通过 volume 挂载）
docker compose -f docker-compose.gatc.prod.yml up -d

# 强制重建
docker compose -f docker-compose.gatc.prod.yml up -d --force-recreate
```

## 配置要求

### 配置文件结构

```
./conf/
├── conf.yaml           # 应用配置（端口等）
├── resource.yaml       # 资源配置（MySQL 连接）
├── gcp/
│   ├── sa-key0.json    # 白账户服务账户密钥
│   ├── gatc_rsa        # SSH 私钥（权限 600）
│   └── gatc_rsa.pub    # SSH 公钥
└── dev/                # 开发环境配置覆盖
    ├── conf.yaml
    └── resource.yaml
```

### 环境检测

`env/env.go` 检测主机名（含 "macbook" 或 "local" 则为开发环境），自动切换配置路径：
- 开发环境：`./conf/dev/`
- 生产环境：`./conf/`

### 配置加载顺序

1. `env.Init()` 检测环境
2. `conf.LoadAppConfig()` 加载应用配置
3. `conf.LoadResourceConf()` 加载资源配置
4. `zlog.InitLogger()` 初始化日志
5. `helpers.InitMysql()` 初始化数据库连接
6. `config.InitGCPConfig()` 初始化 GCP 配置（读取 SSH 密钥和白账户项目 ID）

## API 端点

### VM 管理 (`/api/v1/vm/`)

- `POST /create` - 创建 VM，支持 ForceCreateVm 参数控制复用策略
- `POST /delete` - 删除 VM
- `GET /list` - 分页查询 VM 列表
- `GET /get` - 获取单个 VM 详情
- `POST /refresh-ip` - 刷新 VM 外部 IP
- `POST /replace-proxy-resource` - 替换 VM 代理资源
- `POST /replace-proxy-resource-v2` - 替换 VM 代理资源 V2

### 账户管理 (`/api/v1/account/`)

- `GET /start-registration` - 开始账户注册流程，返回授权 URL 和 session_id
- `GET /submit-auth-key` - 提交 gcloud 授权码完成认证
- `GET /list` - 查询账户列表，支持按状态过滤
- `GET /process-projects` - 执行完整项目处理流程（登录 → 创建项目 → 绑卡 → 获取 Token）
- `GET /process-projects-v2` - 项目处理流程 V2
- `GET /process-projects-v3` - 项目处理流程 V3（最新）
- `POST|GET /set-token-invalid` - 标记 Token 失效
- `GET /emails-with-unbound-projects` - 获取有未绑账单项目的邮箱列表

## 定时任务

`main.go` 中注册的定时任务：

```go
// 每小时清理 24 小时前的 VM
cron.AddFunc("Cleanup 24H ago VMs", "@every 1h", service.GVmService.CleanupOldVMs, -1)

// 每小时同步 VM 状态与 GCP
cron.AddFunc("Sync VMs with GCP", "@every 1h", service.GVmService.SyncVMsWithGCP, 10s)

// 每分钟同步代理信息
cron.AddFunc("Sync Proxys by Vms", "@every 1m", service.SyncProxyByVms, -1)

// 每 10 分钟清理预删除状态的 VM
cron.AddFunc("Cleanup Pending Delete VMs", "@every 10m", service.GVmService.CleanupPendingDeleteVMs, -1)
```

## 核心常量

定义在 `constants/constants.go` 和 `dao/gcp_account.go`：

```go
// VM 配置
DefaultZone           = "us-central1-a"
DefaultMachineType    = "e2-small"
MaxProjectsPerAccount = 12

// 路径配置
WhiteAccountKeyPath = "./conf/gcp/sa-key0.json"
SSHKeyPath          = "./conf/gcp/gatc_rsa"
SSHPubKeyPath       = "./conf/gcp/gatc_rsa.pub"
VMInitScriptPath    = "./scripts/vm_init.sh"

// VM 状态
VMStatusRunning       = 1
VMStatusStopped       = 2
VMStatusDeleted       = 3
VMStatusPendingDelete = 4
```

## 数据库

使用 GORM v2 + MySQL，启动时自动迁移：

```go
helpers.GatcDbClient.AutoMigrate(
    &dao.VMInstance{},
    &dao.GCPAccount{},
)
```

## 日志规范

使用 `base/zlog` 包，支持请求 ID 上下文：

```go
// 带上下文的日志
zlog.InfoWithCtx(c, "message", "key", value)
zlog.ErrorWithCtx(c, "message", err, "key", value)

// 普通日志
zlog.Info("message", "key", value)
zlog.Error("message", err)
```

## Docker 多阶段构建

**Builder 阶段：** `golang:1.22-alpine`
- 下载依赖并编译二进制

**Runtime 阶段：** `gcr.io/google.com/cloudsdktool/google-cloud-cli:latest`
- 预装 gcloud CLI
- 安装 openssh-client、curl、net-tools
- 创建非 root 用户 `gatc`
- 暴露端口 5401
- 健康检查：`GET /health`

## 生产部署目录结构

```
/opt/gatc/
├── conf/                    # 挂载到容器 /app/conf
│   ├── conf.yaml
│   ├── resource.yaml
│   └── gcp/
│       ├── sa-key0.json
│       ├── gatc_rsa
│       └── gatc_rsa.pub
└── mysql/                   # MySQL 数据目录
```

配置文件权限：
- `conf/gcp/` 目录：700
- `gatc_rsa` 私钥：600
