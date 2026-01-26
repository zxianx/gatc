# Proxy Pool 同步功能文档

## 功能概述

实现了从 VM 自动同步代理池的功能，确保 `proxy_pool` 表与实际运行的 VM 保持一致。

## 数据库变更

### proxy_pool 表新增字段

```sql
ALTER TABLE `proxy_pool` ADD COLUMN `from_vm` bigint NOT NULL DEFAULT '0' COMMENT '来源VM标识，0表示非VM来源，>0表示来自VM';
ALTER TABLE `proxy_pool` ADD KEY `idx_from_vm` (`from_vm`);
```

**字段说明**：
- `from_vm`: 来源VM标识（bigint类型）
  - `0`: 非VM来源（手动添加或其他来源）
  - `> 0`: 来自VM（当前使用1标记，将来可以存储具体的vmId）

**为什么使用 bigint 而不是 bool**：
- 预留扩展性，将来可以存储具体的 vmId
- VM 存在历史 VM 和新 VM 共用 IP 的情况，使用数值标识更灵活

**为什么不直接关联 vm_id**：
- VM 存在历史 VM 和新 VM 共用 IP 的情况
- 使用 `from_vm` 标记即可，避免复杂的关联关系

## 代理格式差异

**重要**：两个表的 proxy 字段格式不同：

| 表名 | proxy 格式 | 示例 |
|------|-----------|------|
| vm_instances | `http://IP:1081/px` | `http://35.208.147.190:1081/px` |
| proxy_pool | `http://IP:1081` | `http://35.208.147.190:1081` |

**注意**：在同步时需要去掉 `/px` 后缀。

## 同步逻辑

### SyncProxyPoolFromVMs 方法

**执行流程**：

1. **查询 set1**: 从 `proxy_pool` 表查询 `from_vm > 0` 的记录
2. **查询 set2**: 从 `vm_instances` 表查询 `status = Running` 的 VM
3. **构建映射**: 将 set2 的 proxy 去掉 `/px` 后缀，构建 Map
4. **遍历 set1**:
   - 跳过 `proxy_type` 非 "server" 类型
   - 不在 set2 中的：收集 proxy ID，批量设置为 `ProxyStatusDeleted (9)`
   - 在 set2 中的：标记 VM 为已处理
5. **遍历 set2**:
   - 对未处理的 VM（新增的）：插入新的 ProxyPool 记录，`from_vm = 1`

**代码位置**: [service/vm_service.go:1255-1360](service/vm_service.go#L1255-L1360)

## 集成点

### 1. ReplaceProxyResourceV2 接口

在 V2 版本的替换代理资源接口中，最后一步会自动调用 proxy 同步：

```go
// 步骤4: 同步 proxy_pool
if err := s.SyncProxyPoolFromVMs(c); err != nil {
    zlog.ErrorWithCtx(c, "Failed to sync proxy pool from VMs", err)
    // 不影响主流程，只记录错误
}
```

**位置**: [service/vm_service.go:1193-1200](service/vm_service.go#L1193-L1200)

### 2. ReplaceProxyResource V1 接口

V1 版本在插入新代理时，也会标记 `from_vm = 1`：

```go
newProxies = append(newProxies, dao.ProxyPool{
    Proxy:     proxyURL,
    ProxyType: constants.ProxyTypeHttpProxyAlias,
    Status:    dao.ProxyStatusActive,
    FromVM:    1, // 标记为来自VM
    CreatedAt: time.Now(),
    UpdatedAt: time.Now(),
})
```

**位置**: [service/vm_service.go:1041-1048](service/vm_service.go#L1041-L1048)

## DAO 层新增方法

### ProxyPoolDao

#### GetFromVMProxies
查询来自VM的代理记录（`from_vm > 0`）。

```go
func (d *ProxyPoolDao) GetFromVMProxies(c *gin.Context) ([]ProxyPool, error)
```

**位置**: [dao/proxy_pool.go:109-114](dao/proxy_pool.go#L109-L114)

**说明**：
- 使用现有的 `BatchUpdateStatus` 方法批量更新代理状态
- 通过 proxy ID 列表批量更新，避免逐条更新

## 使用场景

### 自动同步

当调用 `replace-proxy-resource-v2` 接口时，会自动同步 proxy_pool：

```bash
curl -X POST http://localhost:5401/api/v1/vm/replace-proxy-resource-v2 \
  -H "Content-Type: application/json" \
  -d '{
    "num": 3,
    "proxy_type": "server",
    "tag": "batch3"
  }'
```

### 手动同步

如果需要手动触发同步，可以直接调用 service 方法：

```go
err := service.GVmService.SyncProxyPoolFromVMs(c)
```

## 状态说明

### ProxyPool 状态

| 状态值 | 常量名 | 说明 |
|--------|--------|------|
| 0 | ProxyStatusInactive | 未激活 |
| 1 | ProxyStatusActive | 激活中 |
| 2 | ProxyStatusOccupied | 已占用 |
| 9 | ProxyStatusDeleted | 已删除 |

**同步逻辑**：
- 当 VM 不存在时，对应的 proxy 状态会被设置为 `ProxyStatusDeleted (9)`
- 当发现新的 VM 时，会插入新的 proxy 记录，状态为 `ProxyStatusActive (1)`

## 日志示例

```
SyncProxyPoolFromVMs Starting sync proxy pool from VMs
SyncProxyPoolFromVMs Found proxy pool records from VM count=10
SyncProxyPoolFromVMs Found active VMs count=12
SyncProxyPoolFromVMs Built VM proxy map count=12
SyncProxyPoolFromVMs Proxy exists in VMs, marked as processed proxy=http://35.208.147.190:1081
SyncProxyPoolFromVMs Proxy not exists in VMs, mark for deletion proxy=http://35.208.147.191:1081
SyncProxyPoolFromVMs Updated deleted proxies status count=2
SyncProxyPoolFromVMs New proxy to insert proxy=http://35.208.147.192:1081 vmId=gatcvm-server-batch3-0-...
SyncProxyPoolFromVMs Inserted new proxies count=2
SyncProxyPoolFromVMs Sync completed deleted=2 inserted=2
```

## 注意事项

1. **格式转换**: 同步时会自动处理 proxy 格式差异（去掉 `/px` 后缀）
2. **类型过滤**: 只同步 `proxy_type = "server"` 类型的代理
3. **状态过滤**: 只同步 `status = Running` 的 VM
4. **错误处理**: 同步失败不影响主流程，只记录错误日志
5. **幂等性**: 多次执行同步操作是安全的，不会产生重复记录

## 数据一致性

### 保证机制

1. **自动同步**: V2 接口执行后自动同步
2. **状态标记**: 使用 `from_vm` 字段区分来源
3. **批量操作**: 使用批量插入和更新，提高性能
4. **日志记录**: 详细的日志记录，便于追踪和调试

### 潜在问题

1. **IP 复用**: 如果 GCP 回收 IP 并分配给新 VM，可能导致 proxy_pool 中有多条相同 IP 的记录
   - **解决方案**: 使用 `from_vm` 标记，定期清理 `status = deleted` 的记录

2. **并发问题**: 如果同时有多个操作修改 VM 状态，可能导致同步不一致
   - **解决方案**: 建议串行执行替换操作，避免并发冲突

## 未来优化

1. **定时同步**: 可以添加定时任务，定期执行 proxy 同步
2. **增量同步**: 目前是全量同步，可以优化为增量同步
3. **清理机制**: 定期清理 `status = deleted` 的旧记录
4. **监控告警**: 添加同步失败的监控和告警
