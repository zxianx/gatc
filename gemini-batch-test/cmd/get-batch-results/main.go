package main

import (
	"context"
	"flag"
	"fmt"
	"gemini-batch-test/internal/gemini"
	"gemini-batch-test/internal/utils"
	"log"
	"path/filepath"
	"time"
)

func main() {
	var (
		tokenFile  = flag.String("token-file", "", "Token配置文件路径 (必需)")
		jobID      = flag.String("job-id", "", "批处理作业ID (必需)")
		outputDir  = flag.String("output-dir", "output/results", "结果输出目录")
		saveReport = flag.String("save-report", "", "保存详细报告的文件路径 (可选)")
	)
	flag.Parse()

	if *tokenFile == "" {
		log.Fatal("❌ 必须指定token配置文件: -token-file=config/tokens/your-token.json")
	}
	if *jobID == "" {
		log.Fatal("❌ 必须指定作业ID: -job-id=batch_xxxxx")
	}

	fmt.Printf("📥 获取Gemini批处理结果\n")
	fmt.Printf("   Token配置: %s\n", *tokenFile)
	fmt.Printf("   作业ID: %s\n", *jobID)
	fmt.Printf("   输出目录: %s\n", *outputDir)

	// 加载Token配置
	fmt.Printf("\n🔑 正在加载Token配置...\n")
	config, err := utils.LoadTokenConfig(*tokenFile)
	if err != nil {
		log.Fatalf("❌ 加载Token配置失败: %v", err)
	}
	fmt.Printf("✅ Token配置加载成功: %s\n", config.Name)

	// 创建批处理处理器
	processor := gemini.NewBatchProcessor(config.APIKey)

	// 创建上下文
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// 初始化结果结构
	result := &gemini.TestResult{
		JobID:     *jobID,
		StartTime: time.Now(),
	}

	// 获取并分析结果
	fmt.Printf("\n📊 正在获取和分析结果...\n")
	err = processor.GetResults(ctx, *jobID, result)
	if err != nil {
		log.Fatalf("❌ 获取结果失败: %v", err)
	}

	result.EndTime = time.Now()

	// 显示详细统计
	fmt.Printf("\n📈 详细统计报告:\n")
	fmt.Printf("   作业ID: %s\n", result.JobID)
	fmt.Printf("   总请求数: %d\n", result.TotalRequests)
	fmt.Printf("   成功请求: %d\n", result.SuccessfulRequests)
	fmt.Printf("   失败请求: %d\n", result.FailedRequests)
	
	if result.TotalRequests > 0 {
		successRate := float64(result.SuccessfulRequests) / float64(result.TotalRequests) * 100
		fmt.Printf("   成功率: %.2f%%\n", successRate)
	}
	
	if result.ProcessingTime > 0 {
		fmt.Printf("   处理时间: %s\n", utils.FormatDuration(result.ProcessingTime))
		fmt.Printf("   平均单次耗时: %s\n", utils.FormatDuration(result.AvgTimePerRequest))
	}
	
	fmt.Printf("   预估成本节省: %s\n", result.EstimatedCostSaving)

	// 保存详细报告(可选)
	if *saveReport != "" {
		reportPath := *saveReport
		if reportPath == "auto" {
			timestamp := time.Now().Format("20060102-150405")
			reportPath = filepath.Join(*outputDir, fmt.Sprintf("batch-report-%s.json", timestamp))
		}

		if err := utils.SaveTestResult(&utils.TestResult{
			JobID:              result.JobID,
			TotalRequests:      result.TotalRequests,
			SuccessfulRequests: result.SuccessfulRequests,
			FailedRequests:     result.FailedRequests,
			TotalTime:          result.TotalTime.String(),
			ProcessingTime:     result.ProcessingTime.String(),
			AvgTimePerRequest:  result.AvgTimePerRequest.String(),
			EstimatedCostSaving: result.EstimatedCostSaving,
			StartTime:          result.StartTime,
			EndTime:            result.EndTime,
		}, reportPath); err != nil {
			fmt.Printf("⚠️  保存详细报告失败: %v\n", err)
		} else {
			fmt.Printf("💾 详细报告已保存到: %s\n", reportPath)
		}
	}

	// 性能分析和建议
	fmt.Printf("\n🎯 性能分析:\n")
	if result.FailedRequests == 0 {
		fmt.Printf("   ✅ 所有请求都成功处理，批处理作业表现优秀\n")
	} else if float64(result.SuccessfulRequests)/float64(result.TotalRequests) > 0.9 {
		fmt.Printf("   ✅ 成功率大于90%%，批处理作业表现良好\n")
		fmt.Printf("   💡 建议检查失败请求的错误信息，优化请求内容\n")
	} else {
		fmt.Printf("   ⚠️  成功率较低，建议检查以下方面：\n")
		fmt.Printf("      - API密钥权限\n")
		fmt.Printf("      - 请求格式正确性\n")
		fmt.Printf("      - 模型支持情况\n")
	}

	if result.ProcessingTime > 0 {
		avgPerRequest := result.ProcessingTime / time.Duration(result.TotalRequests)
		fmt.Printf("\n⏱️  时间分析:\n")
		fmt.Printf("   平均每请求耗时: %s\n", utils.FormatDuration(avgPerRequest))
		if avgPerRequest < 30*time.Second {
			fmt.Printf("   ✅ 处理速度很快，批处理效率很高\n")
		} else if avgPerRequest < 2*time.Minute {
			fmt.Printf("   ✅ 处理速度正常，批处理效率良好\n")
		} else {
			fmt.Printf("   ⚠️  处理速度较慢，可能是请求复杂度较高\n")
		}
	}

	fmt.Printf("\n💰 成本优势:\n")
	fmt.Printf("   使用Batch API相比实时API预估节省: %s\n", result.EstimatedCostSaving)
	fmt.Printf("   适合大批量、非实时性要求的任务\n")

	fmt.Printf("\n✅ 结果获取和分析完成!\n")
}