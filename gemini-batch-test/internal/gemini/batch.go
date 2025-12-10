package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// BatchProcessor 批处理处理器
type BatchProcessor struct {
	client *Client
}

// NewBatchProcessor 创建批处理处理器
func NewBatchProcessor(apiKey string) *BatchProcessor {
	return &BatchProcessor{
		client: NewClient(apiKey),
	}
}

// CreateJobWithInlineRequests 解析JSONL内容并创建批处理作业 - 使用正确的API格式
func (bp *BatchProcessor) CreateJobWithInlineRequests(ctx context.Context, jsonlContent []byte, fileName string) (*BatchJob, error) {
	// 1. 解析JSONL内容为请求列表
	fmt.Printf("📝 正在解析JSONL内容: %s (大小: %d bytes)\n", fileName, len(jsonlContent))
	requests, err := bp.parseJSONLContent(jsonlContent)
	if err != nil {
		return nil, fmt.Errorf("解析JSONL内容失败: %w", err)
	}

	fmt.Printf("✅ 解析成功，共 %d 个请求\n", len(requests))

	// 2. 创建批处理作业 - 使用正确的内联格式
	fmt.Printf("🚀 正在创建批处理作业...\n")
	batchJob, err := bp.client.CreateBatchJob(ctx, requests)
	if err != nil {
		return nil, fmt.Errorf("创建批处理作业失败: %w", err)
	}

	fmt.Printf("✅ 批处理作业创建成功: %s\n", batchJob.Name)
	fmt.Printf("📊 作业类型: %s\n", batchJob.Metadata.Type)

	return batchJob, nil
}

// parseJSONLContent 解析JSONL内容为请求列表
func (bp *BatchProcessor) parseJSONLContent(jsonlContent []byte) ([]BatchRequest, error) {
	scanner := bufio.NewScanner(bytes.NewReader(jsonlContent))
	var requests []BatchRequest

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var request BatchRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			return nil, fmt.Errorf("解析请求行失败: %w", err)
		}

		requests = append(requests, request)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取JSONL内容失败: %w", err)
	}

	return requests, nil
}

// MonitorJob 监控作业状态
func (bp *BatchProcessor) MonitorJob(ctx context.Context, jobName string, checkInterval time.Duration) (*TestResult, error) {
	result := &TestResult{
		JobID:        jobName,
		StartTime:    time.Now(),
		StateChanges: make([]StateChange, 0),
	}

	fmt.Printf("👁️  开始监控作业: %s\n", jobName)
	fmt.Printf("⏱️  检查间隔: %v\n", checkInterval)

	startTime := time.Now()

	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
			// 查询作业状态
			batchJob, err := bp.client.GetBatchJob(ctx, jobName)
			if err != nil {
				fmt.Printf("❌ 查询作业状态失败: %v\n", err)
				time.Sleep(checkInterval)
				continue
			}

			currentTime := time.Now()
			elapsed := currentTime.Sub(startTime)

			// 显示详细的作业信息
			fmt.Printf("\n📊 作业详细信息 [%s]:\n", currentTime.Format("15:04:05"))
			fmt.Printf("   作业ID: %s\n", batchJob.Name)
			fmt.Printf("   显示名: %s\n", batchJob.Metadata.DisplayName)
			fmt.Printf("   模型: %s\n", batchJob.Metadata.Model)
			fmt.Printf("   类型: %s\n", batchJob.Metadata.Type)
			fmt.Printf("   运行时间: %v\n", elapsed)
			
			// 检查是否有输出结果
			if batchJob.Metadata.Output != nil && batchJob.Metadata.Output.InlinedResponses != nil {
				responseCount := len(batchJob.Metadata.Output.InlinedResponses.InlinedResponses)
				fmt.Printf("   ✅ 作业已完成！共有 %d 个响应结果\n", responseCount)
				
				// 显示每个响应的简要信息
				for i, resp := range batchJob.Metadata.Output.InlinedResponses.InlinedResponses {
					if key, ok := resp.Metadata["key"]; ok {
						fmt.Printf("   📝 响应 %d: %s", i+1, key)
						if resp.Response != nil && len(resp.Response.Candidates) > 0 {
							if resp.Response.UsageMetadata != nil {
								fmt.Printf(" (tokens: %d)", resp.Response.UsageMetadata.TotalTokenCount)
							}
						}
						fmt.Printf("\n")
					}
				}
				
				// 作业已完成，退出监控
				result.EndTime = currentTime
				result.TotalTime = elapsed
				result.TotalRequests = responseCount
				result.SuccessfulRequests = responseCount // 假设都成功了
				result.FailedRequests = 0
				
				fmt.Printf("\n🎉 批处理作业已完成! 总耗时: %v\n", elapsed)
				return result, nil
			} else {
				fmt.Printf("   ⏳ 作业仍在处理中...\n")
			}

			// 如果没有完成，继续等待

			time.Sleep(checkInterval)
		}
	}
}

// GetResults 获取并分析批处理结果 - 使用新的内联响应格式
func (bp *BatchProcessor) GetResults(ctx context.Context, jobName string, result *TestResult) error {
	// 获取作业信息
	batchJob, err := bp.client.GetBatchJob(ctx, jobName)
	if err != nil {
		return fmt.Errorf("获取作业信息失败: %w", err)
	}

	if batchJob.Metadata.Output == nil || batchJob.Metadata.Output.InlinedResponses == nil {
		return fmt.Errorf("作业没有输出结果")
	}

	// 分析内联响应结果
	fmt.Printf("📊 正在分析结果...\n")
	err = bp.parseInlineResults(batchJob.Metadata.Output.InlinedResponses.InlinedResponses, result)
	if err != nil {
		return fmt.Errorf("解析结果失败: %w", err)
	}

	return nil
}

// parseInlineResults 解析内联响应结果
func (bp *BatchProcessor) parseInlineResults(responses []BatchResponseWithMetadata, result *TestResult) error {
	successCount := 0
	failureCount := 0

	for _, resp := range responses {
		if resp.Response != nil && len(resp.Response.Candidates) > 0 {
			successCount++
			if key, ok := resp.Metadata["key"]; ok {
				fmt.Printf("✅ 请求成功 [%s]: ", key)
				if resp.Response.UsageMetadata != nil {
					fmt.Printf("tokens: %d ", resp.Response.UsageMetadata.TotalTokenCount)
				}
				fmt.Printf("字符数: %d\n", len(resp.Response.Candidates[0].Content.Parts[0].Text))
			}
		} else {
			failureCount++
			if key, ok := resp.Metadata["key"]; ok {
				fmt.Printf("❌ 请求失败 [%s]\n", key)
			}
		}
	}

	result.TotalRequests = successCount + failureCount
	result.SuccessfulRequests = successCount
	result.FailedRequests = failureCount

	if result.TotalRequests > 0 && result.TotalTime > 0 {
		result.AvgTimePerRequest = result.TotalTime / time.Duration(result.TotalRequests)
	}

	// 估算成本节省 (Batch API 节省50%)
	result.EstimatedCostSaving = "~50%"

	fmt.Printf("\n📈 结果统计:\n")
	fmt.Printf("   总请求数: %d\n", result.TotalRequests)
	fmt.Printf("   成功请求: %d\n", result.SuccessfulRequests)
	fmt.Printf("   失败请求: %d\n", result.FailedRequests)
	if result.TotalRequests > 0 {
		fmt.Printf("   成功率: %.2f%%\n", float64(successCount)/float64(result.TotalRequests)*100)
	}
	fmt.Printf("   平均单次耗时: %v\n", result.AvgTimePerRequest)
	fmt.Printf("   预估成本节省: %s\n", result.EstimatedCostSaving)

	return nil
}

// parseResults 解析批处理结果
func (bp *BatchProcessor) parseResults(content []byte, result *TestResult) error {
	scanner := bufio.NewScanner(bytes.NewReader(content))

	successCount := 0
	failureCount := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var batchResp BatchResponse
		if err := json.Unmarshal([]byte(line), &batchResp); err != nil {
			fmt.Printf("⚠️  解析响应行失败: %v\n", err)
			continue
		}

		if batchResp.Error != nil {
			failureCount++
			fmt.Printf("❌ 请求失败 [%s]: %s\n", batchResp.Key, batchResp.Error.Message)
		} else if batchResp.Response != nil {
			successCount++
		}
	}

	result.TotalRequests = successCount + failureCount
	result.SuccessfulRequests = successCount
	result.FailedRequests = failureCount

	if result.TotalRequests > 0 {
		result.AvgTimePerRequest = result.ProcessingTime / time.Duration(result.TotalRequests)
	}

	// 估算成本节省 (Batch API 节省50%)
	result.EstimatedCostSaving = "~50%"

	fmt.Printf("\n📈 结果统计:\n")
	fmt.Printf("   总请求数: %d\n", result.TotalRequests)
	fmt.Printf("   成功请求: %d\n", result.SuccessfulRequests)
	fmt.Printf("   失败请求: %d\n", result.FailedRequests)
	fmt.Printf("   成功率: %.2f%%\n", float64(successCount)/float64(result.TotalRequests)*100)
	fmt.Printf("   平均单次耗时: %v\n", result.AvgTimePerRequest)
	fmt.Printf("   预估成本节省: %s\n", result.EstimatedCostSaving)

	return nil
}

// calculateProcessingTime 计算实际处理时间 (RUNNING状态的持续时间)
func (bp *BatchProcessor) calculateProcessingTime(stateChanges []StateChange) time.Duration {
	var runningStart, runningEnd time.Time

	for i, change := range stateChanges {
		if change.State == JobStateRunning {
			runningStart = change.Timestamp
		}
		if change.State == JobStateSucceeded || change.State == JobStateFailed {
			runningEnd = change.Timestamp
			break
		}
		// 如果是最后一个状态且为RUNNING，使用当前时间
		if i == len(stateChanges)-1 && change.State == JobStateRunning {
			runningEnd = time.Now()
		}
	}

	if !runningStart.IsZero() && !runningEnd.IsZero() {
		return runningEnd.Sub(runningStart)
	}

	return 0
}