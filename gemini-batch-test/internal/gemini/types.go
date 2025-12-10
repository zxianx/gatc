package gemini

import (
	"time"
)

// TokenConfig Token配置结构
type TokenConfig struct {
	APIKey      string   `json:"api_key"`
	ProjectID   string   `json:"project_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	UsageNotes  []string `json:"usage_notes,omitempty"`
}

// BatchJobState 批处理作业状态
type BatchJobState string

const (
	JobStatePending   BatchJobState = "JOB_STATE_PENDING"
	JobStateRunning   BatchJobState = "JOB_STATE_RUNNING"
	JobStateSucceeded BatchJobState = "JOB_STATE_SUCCEEDED"
	JobStateFailed    BatchJobState = "JOB_STATE_FAILED"
	JobStateCancelled BatchJobState = "JOB_STATE_CANCELLED"
	JobStateExpired   BatchJobState = "JOB_STATE_EXPIRED"
)

// BatchRequest Batch请求结构 (JSONL格式) - Gemini格式
type BatchRequest struct {
	Key     string      `json:"key"`
	Request RequestBody `json:"request"`
}

// RequestBody Gemini请求体
type RequestBody struct {
	Contents           []Content              `json:"contents"`
	GenerationConfig   *GenerationConfig      `json:"generationConfig,omitempty"`
	SafetySettings     []SafetySetting        `json:"safetySettings,omitempty"`
	SystemInstruction  *SystemInstruction     `json:"systemInstruction,omitempty"`
}

// Content 内容结构
type Content struct {
	Parts []Part `json:"parts"`
	Role  string `json:"role,omitempty"`
}

// Part 内容部分
type Part struct {
	Text string `json:"text"`
}

// BatchJob 批处理作业信息 - 修正为实际API格式
type BatchJob struct {
	Name     string           `json:"name"`
	Metadata BatchJobMetadata `json:"metadata,omitempty"`
}

// BatchJobMetadata 作业元数据
type BatchJobMetadata struct {
	Type        string               `json:"@type"`
	Model       string               `json:"model"`
	DisplayName string               `json:"displayName"`
	Output      *BatchJobOutput      `json:"output,omitempty"`
}

// BatchJobOutput 作业输出
type BatchJobOutput struct {
	InlinedResponses *InlinedResponsesContainer `json:"inlinedResponses,omitempty"`
}

// InlinedResponsesContainer 内联响应容器
type InlinedResponsesContainer struct {
	InlinedResponses []BatchResponseWithMetadata `json:"inlinedResponses"`
}

// BatchResponseWithMetadata 带元数据的批处理响应
type BatchResponseWithMetadata struct {
	Response *GenerateContentResponse `json:"response,omitempty"`
	Metadata map[string]string        `json:"metadata,omitempty"`
}

// InputConfig 输入配置
type InputConfig struct {
	FileName string `json:"fileName"`
}

// OutputConfig 输出配置
type OutputConfig struct {
	FileName string `json:"fileName,omitempty"`
}

// UploadFileResponse 文件上传响应
type UploadFileResponse struct {
	Name         string    `json:"name"`
	DisplayName  string    `json:"displayName,omitempty"`
	MimeType     string    `json:"mimeType"`
	SizeBytes    string    `json:"sizeBytes"`
	CreateTime   time.Time `json:"createTime"`
	UpdateTime   time.Time `json:"updateTime"`
	ExpirationTime time.Time `json:"expirationTime,omitempty"`
	State        string    `json:"state"`
}

// BatchResponse 批处理响应 - Gemini格式
type BatchResponse struct {
	Key      string                 `json:"key"`
	Response *GenerateContentResponse `json:"response,omitempty"`
	Error    *BatchError            `json:"error,omitempty"`
}

// GenerateContentResponse Gemini生成响应
type GenerateContentResponse struct {
	Candidates    []Candidate    `json:"candidates,omitempty"`
	UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
}

// Candidate 候选响应
type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
	Index        int     `json:"index,omitempty"`
}

// UsageMetadata 使用量元数据
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

// BatchError 批处理错误
type BatchError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Status  string `json:"status"`
}

// TestResult 测试结果统计
type TestResult struct {
	JobID              string        `json:"job_id"`
	TotalRequests      int           `json:"total_requests"`
	SuccessfulRequests int           `json:"successful_requests"`
	FailedRequests     int           `json:"failed_requests"`
	TotalTime          time.Duration `json:"total_time"`
	ProcessingTime     time.Duration `json:"processing_time"`
	AvgTimePerRequest  time.Duration `json:"avg_time_per_request"`
	EstimatedCostSaving string        `json:"estimated_cost_saving"`
	StartTime          time.Time     `json:"start_time"`
	EndTime            time.Time     `json:"end_time"`
	StateChanges       []StateChange `json:"state_changes"`
}

// StateChange 状态变化记录
type StateChange struct {
	State     BatchJobState `json:"state"`
	Timestamp time.Time     `json:"timestamp"`
	Duration  time.Duration `json:"duration_from_start"`
}

// GenerationConfig 生成配置
type GenerationConfig struct {
	TopP           *float64        `json:"topP,omitempty"`
	TopK           *int            `json:"topK,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxOutputTokens *int           `json:"maxOutputTokens,omitempty"`
	ThinkingConfig *ThinkingConfig `json:"thinkingConfig,omitempty"`
}

// ThinkingConfig 思考配置
type ThinkingConfig struct {
	IncludeThoughts  bool `json:"includeThoughts"`
	ThinkingBudget   int  `json:"thinkingBudget"`
}

// SafetySetting 安全设置
type SafetySetting struct {
	Category  string `json:"category"`
	Threshold string `json:"threshold"`
}

// SystemInstruction 系统指令
type SystemInstruction struct {
	Parts []Part `json:"parts"`
}

// TestConfig 测试配置
type TestConfig struct {
	RequestCount int               `json:"request_count"`
	RequestType  string            `json:"request_type"` // simple, complex, mixed, advanced
	Model        string            `json:"model"`
	TestName     string            `json:"test_name"`
	Requests     []BatchRequest    `json:"requests"`
}