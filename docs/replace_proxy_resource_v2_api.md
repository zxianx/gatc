# 替换代理资源接口 V2 版本文档

## 接口概述

V2 版本采用更安全的两阶段删除策略：先将旧 VM 标记为"预删除"状态，然后创建新 VM，最后由定时任务负责真正删除旧 VM。这样可以避免在创建新 VM 失败时导致服务中断。

## 接口信息

- **路径**: `/api/v1/vm/replace-proxy-resource-v2`
- **方法**: `POST`
- **Content-Type**: `application/json`

## 请求参数

参数与 V1 版本相同，继承自 `BatchCreateVMParam`：

| 参数名 | 类型 | 必填 | 说明 |
|--------|------|------|------|
| num | int | 是 | 要创建的VM数量，也是要替换的VM数量 |
| zone | string | 否 | GCP区域，默认: us-central1-a |
| machine_type | string | 否 | 机器类型，默认: e2-small |
| tag | string | 否 | VM标签，用于分组标识 |
| proxy_type | string | 是 | 代理类型，**必须是 "server" 或 "httpProxyServer"** |

## 请求示例

```json
{
  "num": 3,
  "zone": "us-central1-a",
  "machine_type": "e2-small",
  "tag": "batch2",
  "proxy_type": "server"
}
```

## 响应结果

### 成功响应

```json
{
  "code": 0,
  "message": "success",
  "data": {
    "marked_pending_delete": 3,
    "new_vms_created": 3,
    "create_vms": {
      "total": 3,
      "success": 3,
      "failed": 0,
      "results": [...]
    },
    "message": "代理资源替换V2完成: 标记预删除VM 3个, 新建VM 3个"
  }
}
```

### 错误响应

```json
{
  "code": 400,
  "message": "proxy_type必须是'server'或'httpProxyServer'，当前值: socks5",
  "data": null
}
```

## 执行流程

V2 版本的执行流程更加简洁和安全：

1. **查询旧VM**: 按 Zone、MachineType、ProxyType 查询最多 `num` 个状态为 Running 的 VM
2. **标记预删除**: 将查询到的 VM 状态设置为 `VMStatusPendingDelete (4)`
3. **创建新VM**: 创建 `num` 个新的代理 VM
4. **等待定时任务**: 由定时任务在 1 小时后真正删除预删除状态的 VM

## VM 状态说明

| 状态值 | 常量名 | 说明 |
|--------|--------|------|
| 1 | VMStatusRunning | 运行中 |
| 2 | VMStatusStopped | 已停止 |
| 3 | VMStatusDeleted | 已删除 |
| 4 | VMStatusPendingDelete | 预删除（新增） |

## 定时任务

### CleanupPendingDeleteVMs

- **执行频率**: 每小时执行一次
- **执行逻辑**:
  1. 查询 `updated_at` 在 1 小时之前且状态为 `VMStatusPendingDelete` 的 VM
  2. 调用批量删除服务真正删除这些 VM
  3. 记录删除结果日志

- **配置常量**: `constants.VMPendingDeleteRetentionHours = 1`

## V1 vs V2 对比

| 特性 | V1 版本 | V2 版本 |
|------|---------|---------|
| 删除策略 | 立即删除（异步70秒后） | 两阶段删除（标记+定时任务） |
| proxy_pool 操作 | 查询、插入、更新、禁用 | 无 |
| official_tokens 操作 | 替换 base_url | 无 |
| 复杂度 | 8个步骤 | 3个步骤 |
| 安全性 | 中等 | 更高 |
| 适用场景 | 需要立即替换代理和更新token | 只需要替换VM，不关心代理池和token |

## 使用建议

### 何时使用 V2

- 只需要替换 VM 资源，不需要更新 proxy_pool 和 official_tokens
- 希望更安全的删除策略，避免误删
- 需要更简单的操作流程

### 何时使用 V1

- 需要同时更新 proxy_pool 表
- 需要替换 official_tokens 表中的代理地址
- 需要完整的代理资源替换流程

## 注意事项

1. **查询条件**: V2 版本按 Zone、MachineType、ProxyType 精确匹配查询 VM
2. **状态限制**: 只查询状态为 Running (1) 的 VM
3. **删除延迟**: VM 标记为预删除后，需要等待 1 小时才会被真正删除
4. **并发安全**: 建议同一时间只执行一次替换操作
5. **定时任务**: 确保定时任务正常运行，否则预删除的 VM 不会被清理

## 数据库变更

### vm_instances 表

- 更新记录: 将旧 VM 的 `status` 设置为 4 (pending_delete)，`updated_at` 更新为当前时间
- 新增记录: 创建新 VM 记录，`status` 为 1 (running)
- 删除记录: 定时任务将预删除状态的 VM `status` 设置为 3 (deleted)

## 监控建议

1. **监控预删除VM数量**: 定期检查 `status = 4` 的 VM 数量，避免堆积
2. **监控定时任务执行**: 确保 `CleanupPendingDeleteVMs` 定时任务正常执行
3. **监控删除成功率**: 关注定时任务的删除成功率和失败原因

## 示例

### 替换 3 个代理 VM

```bash
curl -X POST http://localhost:5401/api/v1/vm/replace-proxy-resource-v2 \
  -H "Content-Type: application/json" \
  -d '{
    "num": 3,
    "zone": "us-central1-a",
    "machine_type": "e2-small",
    "tag": "batch2",
    "proxy_type": "server"
  }'
```

### 查询预删除状态的 VM

```bash
curl "http://localhost:5401/api/v1/vm/list?status=4"
```
