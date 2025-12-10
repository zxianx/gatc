package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LoadTokenConfig 从文件加载Token配置
func LoadTokenConfig(configPath string) (*TokenConfig, error) {
	if configPath == "" {
		return nil, fmt.Errorf("配置文件路径不能为空")
	}

	// 检查文件是否存在
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("配置文件不存在: %s", configPath)
	}

	// 读取文件内容
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 解析JSON
	var config TokenConfig
	if err := json.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 验证必要字段
	if config.APIKey == "" {
		return nil, fmt.Errorf("API Key不能为空")
	}

	if config.APIKey == "AIzaSyXXXXXXXXXXXXXXXXXXXXXXXX" {
		return nil, fmt.Errorf("请填入真实的API Key，当前为示例值")
	}

	return &config, nil
}

// SaveJSONLContent 保存JSONL内容到文件
func SaveJSONLContent(content []byte, outputPath string) error {
	// 确保输出目录存在
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(outputPath, content, 0644); err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	return nil
}

// SaveTestResult 保存测试结果
func SaveTestResult(result *TestResult, outputPath string) error {
	// 确保输出目录存在
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	// 序列化结果
	content, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化测试结果失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(outputPath, content, 0644); err != nil {
		return fmt.Errorf("写入结果文件失败: %w", err)
	}

	return nil
}

// LoadJSONLFile 加载JSONL文件内容
func LoadJSONLFile(filePath string) ([]byte, error) {
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("JSONL文件不存在: %s", filePath)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取JSONL文件失败: %w", err)
	}

	return content, nil
}

// TokenConfig Token配置结构（本地定义避免循环引用）
type TokenConfig struct {
	APIKey      string   `json:"api_key"`
	ProjectID   string   `json:"project_id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	UsageNotes  []string `json:"usage_notes,omitempty"`
}

// TestResult 测试结果结构（本地定义避免循环引用）
type TestResult struct {
	JobID              string        `json:"job_id"`
	TotalRequests      int           `json:"total_requests"`
	SuccessfulRequests int           `json:"successful_requests"`
	FailedRequests     int           `json:"failed_requests"`
	TotalTime          interface{}   `json:"total_time"` // 使用interface{}避免time.Duration序列化问题
	ProcessingTime     interface{}   `json:"processing_time"`
	AvgTimePerRequest  interface{}   `json:"avg_time_per_request"`
	EstimatedCostSaving string        `json:"estimated_cost_saving"`
	StartTime          interface{}   `json:"start_time"`
	EndTime            interface{}   `json:"end_time"`
	StateChanges       []interface{} `json:"state_changes"`
}