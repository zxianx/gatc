

# gcp开号功能

为邮箱账户开启google的api服务，获取api key结果保存。 一个账号可以多次开号。

# 当前逻辑，v3
## 使用方式
1. 用户接口 start-registratio 提供邮箱， 创建起始session ，邮箱绑定一个vm，没有则创建，尝试登陆gcloud， 获取登陆授权码获取url返回给用户
2. 用户登陆，通过接口submit-auth-key 回填授权码，完成登陆
3. 通过接口 process-projects-v3 来执行开猴处理流程 （狭义的开号流程专指这一步）
  
## 开号流程
1. 补充gcp项目
2. 按需解绑旧项目账单
3. 尝试新建项目绑定账单
4. 项目提取api key
5. 后续保存key流程


# 需求，开号流程V4
 
改为半自动开号， 
1. （补充gcp项目 + 按需解绑旧项目账单）合并为接口 createProj_billingUnbind
    参数及默认值： 最大补充项目数3、首次处理项目计入已存在项目 true、是否解绑旧项目true，首次处理项目豁免解绑已存在项目true，
2.  绑定账单未人手动执行，不再用处理
3.  提取api key 及后面key处理 合并为接口 keyWithdraw_keySave·
    无大变换， 当前已绑定账单、未解绑、没有key的项目的尽力提取key

# 注意点
+ 新增v4，不直接改v3， 参数命名复用v3的
+ 支持灵活参数控制， 默认值没注明的按 v3里默认值
+ 原来流程里面有些批量处理项目处理（gcloud 命令）是for逐个调用的， 比较慢， 如果能改成批量的就改。
+ 减少冗余代码，有些步骤做的东西如果可以在其他步骤顺便做了，中间结果缓存在ctx中供后面优先用（没有的话再重复获取），避免冗余，加快执行速度




# 其他
## 授权码保存 （不合理需求，不处理）
~~ 当前授权码没保存，用户尝试2次开后需要重新登陆，该授权码不一定过期， 想要再二次登陆时获取上次授权码尝试 ~~

# V4 设计方案

## 新增文件

1. `service/gcloud/project_process_v4.go` - V4 流程核心实现

## 接口设计

### 1. createProj_billingUnbind 接口

**Handler**: `handler/account_handler.go` - `CreateProjBillingUnbindV4()`
**Service**: `service/gcloud/project_process_v4.go` - `CreateProjBillingUnbindV4()`

**请求参数** (ProjectProcessV4Param):
```go
type ProjectProcessV4Param struct {
    Email                    string `json:"email" form:"email" binding:"required"`
    MaxCreateProjNum         int    `json:"max_create_proj_num" form:"max_create_proj_num"`           // 默认: 3
    FirstTimeProcess         *bool  `json:"first_time_process,omitempty" form:"first_time_process,omitempty"`     // 默认: nil(自动识别)
    FirstTimeCountAsExisting *bool  `json:"first_time_count_as_existing,omitempty" form:"first_time_count_as_existing,omitempty"` // 默认: true，首次处理时已有项目是否计入已存在
    UnbindOldBillingProj     *bool  `json:"unbind_old_billing_proj,omitempty" form:"unbind_old_billing_proj,omitempty"` // 默认: true
    FirstTimeExemptUnbind    *bool  `json:"first_time_exempt_unbind,omitempty" form:"first_time_exempt_unbind,omitempty"` // 默认: true
}
```

**返回结果** (CreateProjBillingUnbindV4Result):
```go
type CreateProjBillingUnbindV4Result struct {
    Email                 string   `json:"email"`
    Success               bool     `json:"success"`
    Message               string   `json:"message"`
    TotalProjects         int      `json:"total_projects"`          // 当前总项目数
    CreatedProjects       int      `json:"created_projects"`        // 新建项目数
    CreatedProjectsDetail []string `json:"created_projects_detail"`
    UnboundProjects       int      `json:"unbound_projects"`        // 解绑项目数
    UnboundProjectsDetail []string `json:"unbound_projects_detail"`
    NeedManualBillingBind bool     `json:"need_manual_billing_bind"` // 是否需要手动绑卡
}
```

**处理流程**:
1. 初始化 WorkCtx（复用 V3 逻辑）
2. 获取 CLI 项目列表（复用 `getCLIProjects`）
3. 判断是否为首次处理（`first_time_process=true` 且项目数<=1）
4. 补充项目到目标数量（`max_create_proj_num` + 当前项目数）
5. 按需解绑旧项目账单：
   - 若 `first_time_process=true` 且 `first_time_exempt_unbind=true`，则跳过解绑
   - 若 `unbind_old_billing_proj=true`，解绑所有已绑账单的项目
6. 同步项目信息到数据库
7. 返回结果，标记需要手动绑卡

### 2. keyWithdraw_keySave 接口

**Handler**: `handler/account_handler.go` - `KeyWithdrawKeySaveV4()`
**Service**: `service/gcloud/project_process_v4.go` - `KeyWithdrawKeySaveV4()`

**请求参数** (KeyWithdrawV4Param):
```go
type KeyWithdrawV4Param struct {
    Email    string `json:"email" form:"email" binding:"required"`
    // 可选：指定处理特定项目，为空则处理所有符合条件的项目
    ProjectIDs []string `json:"project_ids,omitempty" form:"project_ids,omitempty"`
}
```

**返回结果** (KeyWithdrawKeySaveV4Result):
```go
type KeyWithdrawKeySaveV4Result struct {
    Email                 string   `json:"email"`
    Success               bool     `json:"success"`
    Message               string   `json:"message"`
    ProcessedProjects     int      `json:"processed_projects"`      // 处理项目数
    SuccessTokens         int      `json:"success_tokens"`          // 成功获取token数
    FailedTokens          int      `json:"failed_tokens"`           // 失败数
    SuccessProjectsDetail []string `json:"success_projects_detail"`
    FailedProjectsDetail  []string `json:"failed_projects_detail"`
    SyncedTokens          int      `json:"synced_tokens"`           // 同步到official_tokens数
}
```

**处理流程**:
1. 初始化 WorkCtx（复用 V3 逻辑）
2. 查询数据库获取符合条件的项目：
   - 已绑定账单（`billing_status = 1`）
   - 未解绑（`billing_status != 2`）
   - 没有key（`token_status = 0`）
3. 遍历项目，对每个项目：
   - 启用必要 API 服务（services enable，复用 V3 `generateTokenForProject`）
   - 创建 API Key（复用 V3 逻辑）
   - 更新数据库 token_status 和 official_token
4. 同步 token 到 official_tokens 表（复用 V3 `PostLoginProcessStep5TokenSync`）
5. 返回结果

## 路由注册

在 `main.go` 中添加：
```go
account.POST("/create-proj-billing-unbind-v4", accountHandler.CreateProjBillingUnbindV4)
account.POST("/key-withdraw-key-save-v4", accountHandler.KeyWithdrawKeySaveV4)
```

## 复用与优化

### 复用 V3 的函数
1. `getCLIProjects()` - 获取 CLI 项目列表
2. `loadDBProjects()` - 加载数据库项目
3. `createProjects()` - 批量创建项目
4. `generateTokenForProject()` - 为项目生成 Token
5. `unbindProjectBilling()` - 解绑项目账单
6. `PostLoginProcessStep5TokenSync()` - Token 同步到 official_tokens

### 优化点
1. **批量命令优化**: 对于 services enable，可以研究是否支持批量启用
2. **缓存中间结果**: 在 Handler 中缓存 WorkCtx 避免重复初始化（如需连续调用两个接口）
3. **减少重复查询**: 两个接口独立，不共享 ctx，但内部复用 V3 的查询函数

## 接口调用流程

```
1. 用户登录（复用 /start-registration + /submit-auth-key）
   ↓
2. POST /create-proj-billing-unbind-v4
   - 自动补充项目
   - 自动解绑旧账单（按需）
   - 返回：需要手动绑卡的提示
   ↓
3. 【手动操作】用户在 GCP 控制台绑定账单
   ↓
4. POST /key-withdraw-key-save-v4
   - 自动提取所有已绑卡项目的 API Key
   - 自动保存到 official_tokens
```