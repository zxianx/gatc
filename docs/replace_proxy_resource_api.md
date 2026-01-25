# 替换代理资源接口文档

## 接口概述

该接口用于自动化替换代理资源，包括创建新的VM代理机、更新代理池、替换token中的代理地址，以及删除旧的VM。

## 接口信息

- **路径**: `/api/v1/vm/replace-proxy-resource`
- **方法**: `POST`
- **Content-Type**: `application/json`

## 请求参数

参数继承自 `BatchCreateVMParam`，包含以下字段：

| 参数名 | 类型 | 必填 | 说明 |
|--------|------|------|------|
| num | int | 是 | 要创建的VM数量，也是要替换的代理数量 |
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
  "tag": "batch1",
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
    "new_vms_created": 3,
    "new_proxies_added": 3,
    "old_proxies_disabled": 3,
    "tokens_updated": 15,
    "old_vms_deleted": 3,
    "deleted_vm_ids": ["gatcvm-server-batch0-0-...", "gatcvm-server-batch0-1-...", "gatcvm-server-batch0-2-..."],
    "message": "代理资源替换完成: 新建VM 3个, 新增代理 3个, 禁用旧代理 3个, 更新Token 15个, 删除旧VM 3个"
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

该接口会按以下步骤自动执行：

1. **验证参数**: 检查 `proxy_type` 必须是 "server" 或 "httpProxyServer"
2. **创建新VM**: 创建 `num` 个新的代理VM
3. **查询旧代理**: 从 `proxy_pool` 表查询最近的 `num` 个活跃代理（按 `created_at` 倒序）
4. **插入新代理**: 将新VM的代理信息插入 `proxy_pool` 表
   - 代理格式: `http://IP:1081` (不含 /px 后缀)
   - `proxy_type`: "server"
   - `status`: 1 (active)
5. **建立映射**: 建立新旧代理的 1:1 映射关系
6. **更新Token**: 在 `official_tokens` 表中，将所有使用旧代理的 `base_url` 替换为新代理
   - 查询条件: `base_url LIKE "http://旧代理/px%"`
   - 替换: 将旧代理地址替换为新代理地址
7. **禁用旧代理**: 将旧代理在 `proxy_pool` 表中的 `status` 设置为 0
8. **删除旧VM**: 删除旧代理对应的VM实例

## 数据库变更

### proxy_pool 表

- 新增记录: 插入新代理信息
- 更新记录: 将旧代理 `status` 设置为 0

### official_tokens 表

- 更新记录: 替换 `base_url` 中的代理地址

### vm_instances 表

- 新增记录: 创建新VM记录
- 更新记录: 将旧VM `status` 设置为 3 (deleted)

## 注意事项

1. **proxy_type 限制**: 该接口只支持 "server" 和 "httpProxyServer" 类型的代理，不支持 socks5
2. **并发安全**: 建议同一时间只执行一次替换操作，避免并发冲突
3. **失败处理**: 如果某些步骤失败，已完成的步骤不会回滚，但会在日志中记录详细错误
4. **VM创建时间**: 创建VM需要一定时间，如果VM数量较多，整个过程可能需要几分钟
5. **IP获取**: 新创建的VM可能需要等待几秒才能获取到外网IP

## 相关表结构

### proxy_pool

```sql
CREATE TABLE `proxy_pool` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `status` bigint NOT NULL DEFAULT '0',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `proxy` varchar(255) NOT NULL DEFAULT '' COMMENT 'proxy地址',
  `proxy_type` varchar(16) NOT NULL DEFAULT '' COMMENT 'server或socks5',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_proxy_status` (`proxy`,`status`),
  KEY `idx_proxy_type_status` (`proxy_type`,`status`),
  KEY `idx_created_at` (`created_at`)
);
```

### vm_instances

- `proxy` 字段格式: `http://IP:1081/px` (含 /px 后缀)
- `proxy_type`: "httpProxyServer" 或 "server"

### official_tokens

- `base_url` 字段格式: `http://IP:1081/px...` (含 /px 后缀及其他路径)
