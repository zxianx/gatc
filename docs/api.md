# GATC API 接口文档

本文档描述 GATC (Google Account Token Collector) 的 HTTP API 接口。

## 基础信息

- **基础 URL**: `http://localhost:5401`
- **API 前缀**: `/api/v1`
- **Content-Type**: `application/json`

---

## 账户管理接口

### 删除账户

硬删除指定邮箱的 GCP 账户及其下属所有项目记录。

**请求信息**

| 项目 | 内容 |
|------|------|
| 接口地址 | `POST /api/v1/account/delete` |
| 请求方式 | POST |
| Content-Type | application/json |

**请求参数**

支持单个删除或批量删除，两种方式互斥，优先判断 `emails` 字段。

| 参数名 | 类型 | 必填 | 说明 |
|--------|------|------|------|
| email | string | 条件 | 单个邮箱地址，删除该邮箱下所有记录 |
| emails | []string | 条件 | 邮箱地址列表，批量删除多个邮箱 |

> 注意：`email` 和 `emails` 至少提供一个。

**请求示例**

单个删除：

```bash
curl -X POST http://localhost:5401/api/v1/account/delete \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com"
  }'
```

批量删除：

```bash
curl -X POST http://localhost:5401/api/v1/account/delete \
  -H "Content-Type: application/json" \
  -d '{
    "emails": [
      "user1@example.com",
      "user2@example.com",
      "user3@example.com"
    ]
  }'
```

**响应参数**

| 参数名 | 类型 | 说明 |
|--------|------|------|
| code | int | 状态码，200 表示成功 |
| message | string | 响应消息 |
| data | object | 响应数据 |
| data.deleted_count | int64 | 实际删除的数据库记录数 |
| data.emails | []string | 被删除的邮箱列表 |

**响应示例**

成功响应：

```json
{
  "code": 200,
  "message": "删除成功",
  "data": {
    "deleted_count": 5,
    "emails": [
      "user@example.com"
    ]
  }
}
```

批量删除成功响应：

```json
{
  "code": 200,
  "message": "删除成功",
  "data": {
    "deleted_count": 12,
    "emails": [
      "user1@example.com",
      "user2@example.com",
      "user3@example.com"
    ]
  }
}
```

错误响应：

```json
{
  "code": 400,
  "message": "邮箱不能为空，请提供 email 或 emails 参数",
  "data": null
}
```

**业务说明**

1. **硬删除**：该接口使用数据库硬删除（`DELETE`），数据不可恢复
2. **级联删除**：删除指定邮箱会同时删除：
   - 该邮箱的账户状态记录（project_id 为空的记录）
   - 该邮箱下的所有项目记录（project_id 非空的记录）
3. **批量原子性**：批量删除时，每个邮箱独立处理，部分失败不影响其他邮箱

**状态码说明**

| HTTP 状态码 | 说明 |
|------------|------|
| 200 | 删除成功 |
| 400 | 请求参数错误（邮箱为空） |
| 500 | 服务器内部错误（数据库操作失败） |

---

### V4 创建项目并解绑账单

V4 半自动开号流程的第一步：补充 GCP 项目、检查账单状态、按需解绑旧项目账单。绑定账单改为手动操作。

**请求信息**

| 项目 | 内容 |
|------|------|
| 接口地址 | `POST /api/v1/account/create-proj-billing-unbind-v4` |
| 请求方式 | POST |
| Content-Type | application/json |

**请求参数**

| 参数名 | 类型 | 必填 | 默认值 | 说明 |
|--------|------|------|--------|------|
| email | string | 是 | - | 邮箱地址 |
| max_create_proj_num | int | 否 | 3 | 最大补充项目数 |
| first_time_process | bool | 否 | nil(自动识别) | 是否首次处理，nil 时自动识别（项目数<=1为首次） |
| first_time_count_as_existing | bool | 否 | true | 首次处理时已有项目是否计入已存在（减少创建数） |
| unbind_old_billing_proj | bool | 否 | true | 是否解绑旧项目账单 |
| first_time_exempt_unbind | bool | 否 | true | 首次处理是否豁免解绑（避免误解绑已有账单） |

**请求示例**

```bash
curl -X POST http://localhost:5401/api/v1/account/create-proj-billing-unbind-v4 \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "max_create_proj_num": 3,
    "unbind_old_billing_proj": true,
    "first_time_exempt_unbind": true
  }'
```

**响应参数**

| 参数名 | 类型 | 说明 |
|--------|------|------|
| code | int | 状态码，200 表示成功 |
| message | string | 响应消息 |
| data | object | 响应数据 |
| data.email | string | 邮箱地址 |
| data.success | bool | 是否成功 |
| data.total_projects | int | 当前总项目数 |
| data.created_projects | int | 新建项目数 |
| data.created_projects_detail | []string | 新建项目ID列表 |
| data.unbound_projects | int | 解绑项目数 |
| data.unbound_projects_detail | []string | 解绑项目ID列表 |
| data.need_manual_billing_bind | bool | 是否需要手动绑卡（固定为true） |

**响应示例**

成功响应：

```json
{
  "code": 200,
  "message": "完成: 总项目5, 新建3, 解绑2",
  "data": {
    "email": "user@example.com",
    "success": true,
    "total_projects": 5,
    "created_projects": 3,
    "created_projects_detail": [
      "gatc-project-123456-123456",
      "gatc-project-123456-234567",
      "gatc-project-123456-345678"
    ],
    "unbound_projects": 2,
    "unbound_projects_detail": [
      "old-project-1",
      "old-project-2"
    ],
    "need_manual_billing_bind": true
  }
}
```

**业务流程**

1. 获取 CLI 项目列表
2. 获取 DB 记录，跳过 DB 中已解绑的项目不检查账单
3. 检查实际账单状态，记录到内存
4. 按需创建新项目（标记 NewCreate）
5. 同步所有项目到 DB（使用准确的 billing 状态）
6. 按需解绑（只解绑 BillingAccount 不为空且非新建的项目）

**状态码说明**

| HTTP 状态码 | 说明 |
|------------|------|
| 200 | 处理成功 |
| 400 | 请求参数错误 |
| 500 | 服务器内部错误 |

---

### V4 提取 API Key 并保存

V4 半自动开号流程的第二步：对已绑定账单的项目提取 API Key 并保存到 official_tokens。如果提取失败会自动触发异步同步修复数据。

**请求信息**

| 项目 | 内容 |
|------|------|
| 接口地址 | `POST /api/v1/account/key-withdraw-key-save-v4` |
| 请求方式 | POST |
| Content-Type | application/json |

**请求参数**

| 参数名 | 类型 | 必填 | 说明 |
|--------|------|------|------|
| email | string | 是 | 邮箱地址 |
| project_ids | []string | 否 | 指定处理的项目ID列表，为空则处理所有符合条件的项目 |

**请求示例**

处理所有符合条件的项目：

```bash
curl -X POST http://localhost:5401/api/v1/account/key-withdraw-key-save-v4 \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com"
  }'
```

指定项目：

```bash
curl -X POST http://localhost:5401/api/v1/account/key-withdraw-key-save-v4 \
  -H "Content-Type: application/json" \
  -d '{
    "email": "user@example.com",
    "project_ids": ["project-1", "project-2"]
  }'
```

**响应参数**

| 参数名 | 类型 | 说明 |
|--------|------|------|
| code | int | 状态码，200 表示成功 |
| message | string | 响应消息 |
| data | object | 响应数据 |
| data.email | string | 邮箱地址 |
| data.success | bool | 是否有成功提取的 token |
| data.processed_projects | int | 处理项目数 |
| data.success_tokens | int | 成功获取 token 数 |
| data.failed_tokens | int | 失败数 |
| data.success_projects_detail | []string | 成功项目ID列表 |
| data.failed_projects_detail | []string | 失败项目ID列表 |
| data.synced_tokens | int | 同步到 official_tokens 数 |

**响应示例**

成功响应：

```json
{
  "code": 200,
  "message": "完成: 处理5, 成功3, 失败2, 同步3",
  "data": {
    "email": "user@example.com",
    "success": true,
    "processed_projects": 5,
    "success_tokens": 3,
    "failed_tokens": 2,
    "success_projects_detail": [
      "project-1",
      "project-2",
      "project-3"
    ],
    "failed_projects_detail": [
      "project-4",
      "project-5"
    ],
    "synced_tokens": 3
  }
}
```

**业务说明**

1. **处理条件**：查询 DB 中 `billing_status=1`(已绑定) 且 `token_status=0`(无token) 的项目
2. **自动同步机制**：
   - 如果有失败项目，或成功数小于处理数，会自动触发异步同步
   - 同步使用 `CreateProjBillingUnbindV4` 特殊参数（`MaxCreateProjNum=0`, `UnbindOldBillingProj=false`）
   - 目的是修复可能的外部干预导致的数据不一致
3. **重试建议**：如果返回有失败项目，建议等待几秒后重试

**V4 完整流程**

```
1. POST /create-proj-billing-unbind-v4
   - 自动补充项目、解绑旧账单
   - 返回需要手动绑卡的提示
   ↓
2. 【手动操作】在 GCP 控制台绑定账单
   ↓
3. POST /key-withdraw-key-save-v4
   - 自动提取所有已绑卡项目的 API Key
   - 自动保存到 official_tokens
   - 失败时自动触发同步修复
```

**状态码说明**

| HTTP 状态码 | 说明 |
|------------|------|
| 200 | 处理完成（可能有部分失败） |
| 400 | 请求参数错误 |
| 500 | 服务器内部错误 |
