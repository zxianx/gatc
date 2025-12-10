# 批处理功能测试

## 文件结构
```
vmHttpServerProxy/
├── main.go     # 核心代理服务器逻辑
├── batch.go    # Gemini异步批处理功能实现
├── env.go      # 环境变量配置
├── go.mod      # 模块定义
├── README.md   # 项目文档
└── testServer.go # 测试服务器
```

## Gemini批处理功能说明

### 触发条件
1. 请求头包含 `X-Gemini-Batch: 1`
2. 目标URL包含 `v1beta/models/gemini`

### 批处理流程
1. **收集阶段**: 2分钟内最多收集20个请求
2. **提交阶段**: 统一提交到 `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:batchGenerateContent`
3. **轮询阶段**: 每10秒查询一次状态，直到完成或超时
4. **分发阶段**: 按key匹配结果，返回给各个原始请求

### 超时配置
- **HTTP客户端超时**: 30秒（仅用于提交和查询）
- **轮询总超时**: 2分钟 + 请求数 × 30秒
- **轮询间隔**: 10秒

### 环境变量配置
```bash
export BatchCollectTimeout=120      # 收集超时（秒），默认120
export BatchMaxSize=20              # 最大批次大小，默认20
export Debug=true                   # 调试模式，默认false
```

## 测试方法

### 1. 启动代理服务器
```bash
go run . 
# 或指定环境变量
BatchCollectTimeout=60 BatchMaxSize=10 go run .
```

### 2. 测试Gemini批处理请求

需要真实的Gemini API Key进行测试：

```bash
# 单个批处理请求
curl -X POST "http://localhost:1081/px/https%3A%2F%2Fgenerativelanguage.googleapis.com%2Fv1beta%2Fmodels%2Fgemini-2.5-flash%3AgenerateContent" \
  -H "Content-Type: application/json" \
  -H "X-Gemini-Batch: 1" \
  -H "X-Goog-Api-Key: YOUR_API_KEY" \
  -d '{"contents":[{"parts":[{"text":"解释量子纠缠"}]}]}'

# 快速发送多个批处理请求
for i in {1..3}; do
  curl -X POST "http://localhost:1081/px/https%3A%2F%2Fgenerativelanguage.googleapis.com%2Fv1beta%2Fmodels%2Fgemini-2.5-flash%3AgenerateContent" \
    -H "Content-Type: application/json" \
    -H "X-Gemini-Batch: 1" \
    -H "X-Goog-Api-Key: YOUR_API_KEY" \
    -d "{\"contents\":[{\"parts\":[{\"text\":\"解释物理问题 $i\"}]}]}" &
done
wait
```

### 3. 监控日志输出

服务器会输出以下类型的日志（所有日志都带有批次ID便于追踪）：
```
2025/12/10 [Batch 1] 创建新批次
2025/12/10 [Batch 1] 添加请求: req_b_1_i_0, 当前数量: 1/20
2025/12/10 [Batch 1] 添加请求: req_b_1_i_1, 当前数量: 2/20
2025/12/10 [Batch 1] 添加请求: req_b_1_i_2, 当前数量: 3/20
2025/12/10 [Batch 1] 执行批处理: https://..., 请求数量: 3
2025/12/10 [Batch 1] 批处理作业已创建: batches/4m855v7q30kc7gtzcigdg9m72o87s08w33vp
2025/12/10 [Batch 1] 批处理作业状态: BATCH_STATE_PENDING
2025/12/10 [Batch 1] 批处理作业状态: BATCH_STATE_RUNNING
2025/12/10 [Batch 1] 批处理作业状态: BATCH_STATE_SUCCEEDED
2025/12/10 [Batch 1] 批处理作业完成: 耗时=90.23s
2025/12/10 [Batch 1] 成功提取3个响应结果
2025/12/10 [Batch 1] 分发响应: req_b_1_i_0
2025/12/10 [Batch 1] 分发响应: req_b_1_i_1
2025/12/10 [Batch 1] 分发响应: req_b_1_i_2
```

**日志说明:**
- `[Batch N]`: 批次号，全局自增，方便追踪
- `req_b_{batch_id}_i_{idx}`: 请求Key格式，包含批次ID和批内索引

## 预期行为

1. 带`X-Gemini-Batch: 1`且URL包含`v1beta/models/gemini`的请求进入批处理队列
2. 忽略原始URL，统一使用`gemini-2.5-flash:batchGenerateContent`端点
3. 使用首个请求的认证头（`X-Goog-Api-Key`或`Authorization`）
4. 构造Gemini批处理格式提交作业
5. 轮询状态直到完成（PENDING → RUNNING → SUCCEEDED）
6. 从`metadata.output.inlinedResponses.inlinedResponses`提取结果
7. 按key分发响应给各个原始请求

## 错误处理

- **提交失败**: 返回 429 Too Many Requests
- **轮询超时**: 返回 504 Gateway Timeout
- **作业失败**: 返回 500 Internal Server Error
- **结果缺失**: 返回包含错误信息的JSON