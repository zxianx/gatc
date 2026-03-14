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
