package main

import (
	"context"
	"flag"
	"fmt"
	"gemini-batch-test/internal/gemini"
	"gemini-batch-test/internal/testdata"
	"gemini-batch-test/internal/utils"
	"log"
	"path/filepath"
	"time"
)

func main() {
	var (
		tokenFile = flag.String("token-file", "", "Token配置文件路径 (必需)")
		count     = flag.Int("count", 10, "生成请求的数量")
		reqType   = flag.String("type", "simple", "请求类型 (simple, complex, mixed, advanced)")
		model     = flag.String("model", "gemini-2.5-flash", "使用的Gemini模型")
		interval  = flag.Int("interval", 10, "状态检查间隔(秒)")
		saveAll   = flag.Bool("save-all", true, "保存所有中间结果")
		outputDir = flag.String("output-dir", "output", "输出根目录")
	)
	flag.Parse()

	if *tokenFile == "" {
		log.Fatal("❌ 必须指定token配置文件: -token-file=config/tokens/your-token.json")
	}

	timestamp := time.Now().Format("20060102-150405")
	testName := fmt.Sprintf("gemini-batch-test-%s-%d-%s", *reqType, *count, timestamp)

	fmt.Printf("🚀 Gemini批处理完整测试\n")
	fmt.Printf("   测试名称: %s\n", testName)
	fmt.Printf("   Token配置: %s\n", *tokenFile)
	fmt.Printf("   请求数量: %d\n", *count)
	fmt.Printf("   请求类型: %s\n", *reqType)
	fmt.Printf("   模型: %s\n", *model)

	// 加载Token配置
	fmt.Printf("\n🔑 正在加载Token配置...\n")
	config, err := utils.LoadTokenConfig(*tokenFile)
	if err != nil {
		log.Fatalf("❌ 加载Token配置失败: %v", err)
	}
	fmt.Printf("✅ Token配置加载成功: %s\n", config.Name)

	// 创建批处理处理器
	processor := gemini.NewBatchProcessor(config.APIKey)

	// 第1步：生成测试数据
	fmt.Printf("\n📝 步骤1: 生成测试数据\n")
	generator := testdata.NewTestDataGenerator(*model)
	jsonlContent, err := generator.GenerateJSONL(*reqType, *count)
	if err != nil {
		log.Fatalf("❌ 生成测试数据失败: %v", err)
	}
	fmt.Printf("✅ 生成 %d 个 %s 类型的请求 (%.2f KB)\n", *count, *reqType, float64(len(jsonlContent))/1024)

	// 保存测试数据
	if *saveAll {
		testDataPath := filepath.Join(*outputDir, "test-data", fmt.Sprintf("%s.jsonl", testName))
		if err := utils.SaveJSONLContent(jsonlContent, testDataPath); err != nil {
			fmt.Printf("⚠️  保存测试数据失败: %v\n", err)
		} else {
			fmt.Printf("💾 测试数据已保存到: %s\n", testDataPath)
		}
	}

	// 第2步：创建批处理作业
	fmt.Printf("\n🚀 步骤2: 创建批处理作业\n")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fileName := fmt.Sprintf("%s.jsonl", testName)
	batchJob, err := processor.CreateJobWithInlineRequests(ctx, jsonlContent, fileName)
	if err != nil {
		log.Fatalf("❌ 创建批处理作业失败: %v", err)
	}

	fmt.Printf("✅ 批处理作业创建成功: %s\n", batchJob.Name)

	// 第3步：监控作业状态
	fmt.Printf("\n👁️  步骤3: 监控作业状态\n")
	ctx, cancel = context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()

	checkInterval := time.Duration(*interval) * time.Second
	result, err := processor.MonitorJob(ctx, batchJob.Name, checkInterval)
	if err != nil {
		log.Fatalf("❌ 监控作业失败: %v", err)
	}

	fmt.Printf("✅ 作业监控完成，总耗时: %s\n", utils.FormatDuration(result.TotalTime))

	// 第4步：获取和分析结果
	fmt.Printf("\n📊 步骤4: 获取和分析结果\n")
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	err = processor.GetResults(ctx, batchJob.Name, result)
	if err != nil {
		log.Fatalf("❌ 获取结果失败: %v", err)
	}

	result.EndTime = time.Now()

	// 生成综合报告
	fmt.Printf("\n📈 完整测试报告\n")
	fmt.Printf("=====================================\n")
	fmt.Printf("测试名称: %s\n", testName)
	fmt.Printf("测试配置: %d个%s类型请求 (%s模型)\n", *count, *reqType, *model)
	fmt.Printf("作业ID: %s\n", result.JobID)
	fmt.Printf("\n⏱️  时间统计:\n")
	fmt.Printf("   开始时间: %s\n", utils.FormatTime(result.StartTime))
	fmt.Printf("   结束时间: %s\n", utils.FormatTime(result.EndTime))
	fmt.Printf("   总耗时: %s\n", utils.FormatDuration(result.TotalTime))
	if result.ProcessingTime > 0 {
		fmt.Printf("   实际处理时间: %s\n", utils.FormatDuration(result.ProcessingTime))
		fmt.Printf("   平均单次耗时: %s\n", utils.FormatDuration(result.AvgTimePerRequest))
	}

	fmt.Printf("\n📊 成功统计:\n")
	fmt.Printf("   总请求数: %d\n", result.TotalRequests)
	fmt.Printf("   成功请求: %d\n", result.SuccessfulRequests)
	fmt.Printf("   失败请求: %d\n", result.FailedRequests)
	if result.TotalRequests > 0 {
		successRate := float64(result.SuccessfulRequests) / float64(result.TotalRequests) * 100
		fmt.Printf("   成功率: %.2f%%\n", successRate)
	}
	fmt.Printf("   预估成本节省: %s\n", result.EstimatedCostSaving)

	fmt.Printf("\n🔄 状态变化历史:\n")
	for _, change := range result.StateChanges {
		fmt.Printf("   [%s] %s (距开始: %s)\n",
			utils.FormatTime(change.Timestamp),
			change.State,
			utils.FormatDuration(change.Duration))
	}

	// 保存综合报告
	if *saveAll {
		reportPath := filepath.Join(*outputDir, "reports", fmt.Sprintf("%s-report.json", testName))
		if err := utils.SaveTestResult(&utils.TestResult{
			JobID:               result.JobID,
			TotalRequests:       result.TotalRequests,
			SuccessfulRequests:  result.SuccessfulRequests,
			FailedRequests:      result.FailedRequests,
			TotalTime:           result.TotalTime.String(),
			ProcessingTime:      result.ProcessingTime.String(),
			AvgTimePerRequest:   result.AvgTimePerRequest.String(),
			EstimatedCostSaving: result.EstimatedCostSaving,
			StartTime:           result.StartTime,
			EndTime:             result.EndTime,
		}, reportPath); err != nil {
			fmt.Printf("⚠️  保存综合报告失败: %v\n", err)
		} else {
			fmt.Printf("\n💾 综合报告已保存到: %s\n", reportPath)
		}
	}

	// 性能总结和建议
	fmt.Printf("\n🎯 测试总结:\n")
	if result.FailedRequests == 0 {
		fmt.Printf("   ✅ 测试完美通过! 所有请求都成功处理\n")
	} else if float64(result.SuccessfulRequests)/float64(result.TotalRequests) > 0.9 {
		fmt.Printf("   ✅ 测试表现良好! 成功率超过90%%\n")
	} else {
		fmt.Printf("   ⚠️  测试结果需要优化，成功率偏低\n")
	}

	if result.ProcessingTime > 0 {
		avgPerRequest := result.ProcessingTime / time.Duration(result.TotalRequests)
		fmt.Printf("   ⏱️  平均处理速度: %s/请求\n", utils.FormatDuration(avgPerRequest))
	}

	fmt.Printf("   💰 成本优势: Batch API比实时API节省约50%%费用\n")

	fmt.Printf("\n💡 使用建议:\n")
	fmt.Printf("   - 适用场景: 大批量、非实时性要求的AI任务\n")
	fmt.Printf("   - 最佳实践: 单次50-200个请求，充分利用批处理效率\n")
	fmt.Printf("   - 监控策略: 使用较长的检查间隔(10-60秒)减少API调用\n")

	fmt.Printf("\n🎉 Gemini批处理完整测试完成!\n")
}
