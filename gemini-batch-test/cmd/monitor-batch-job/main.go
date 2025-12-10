package main

import (
	"context"
	"flag"
	"fmt"
	"gemini-batch-test/internal/gemini"
	"gemini-batch-test/internal/utils"
	"log"
	"time"
)

func main() {
	var (
		tokenFile = flag.String("token-file", "", "Token配置文件路径 (必需)")
		jobID     = flag.String("job-id", "", "批处理作业ID (必需)")
		interval  = flag.Int("interval", 10, "检查间隔(秒)")
		saveResult = flag.String("save-result", "", "保存监控结果的文件路径 (可选)")
	)
	flag.Parse()

	if *tokenFile == "" {
		log.Fatal("❌ 必须指定token配置文件: -token-file=config/tokens/your-token.json")
	}
	if *jobID == "" {
		log.Fatal("❌ 必须指定作业ID: -job-id=batch_xxxxx")
	}

	fmt.Printf("👁️  监控Gemini批处理作业\n")
	fmt.Printf("   Token配置: %s\n", *tokenFile)
	fmt.Printf("   作业ID: %s\n", *jobID)
	fmt.Printf("   检查间隔: %d秒\n", *interval)

	// 加载Token配置
	fmt.Printf("\n🔑 正在加载Token配置...\n")
	config, err := utils.LoadTokenConfig(*tokenFile)
	if err != nil {
		log.Fatalf("❌ 加载Token配置失败: %v", err)
	}
	fmt.Printf("✅ Token配置加载成功: %s\n", config.Name)

	// 创建批处理处理器
	processor := gemini.NewBatchProcessor(config.APIKey)

	// 创建上下文 (最长等待24小时)
	ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
	defer cancel()

	// 监控作业状态
	checkInterval := time.Duration(*interval) * time.Second
	result, err := processor.MonitorJob(ctx, *jobID, checkInterval)
	if err != nil {
		log.Fatalf("❌ 监控作业失败: %v", err)
	}

	// 显示最终结果
	fmt.Printf("\n📊 监控结果:\n")
	fmt.Printf("   作业ID: %s\n", result.JobID)
	fmt.Printf("   开始时间: %s\n", utils.FormatTime(result.StartTime))
	fmt.Printf("   结束时间: %s\n", utils.FormatTime(result.EndTime))
	fmt.Printf("   总耗时: %s\n", utils.FormatDuration(result.TotalTime))
	if result.ProcessingTime > 0 {
		fmt.Printf("   处理耗时: %s\n", utils.FormatDuration(result.ProcessingTime))
	}

	fmt.Printf("\n🔄 状态变化历史:\n")
	for _, change := range result.StateChanges {
		fmt.Printf("   [%s] %s (距开始: %s)\n",
			utils.FormatTime(change.Timestamp),
			change.State,
			utils.FormatDuration(change.Duration))
	}

	// 保存监控结果(可选)
	if *saveResult != "" {
		if err := utils.SaveTestResult(&utils.TestResult{
			JobID:              result.JobID,
			TotalTime:          result.TotalTime.String(),
			ProcessingTime:     result.ProcessingTime.String(),
			StartTime:          result.StartTime,
			EndTime:            result.EndTime,
		}, *saveResult); err != nil {
			fmt.Printf("⚠️  保存监控结果失败: %v\n", err)
		} else {
			fmt.Printf("💾 监控结果已保存到: %s\n", *saveResult)
		}
	}

	// 显示下一步操作
	fmt.Printf("\n💡 下一步操作:\n")
	fmt.Printf("   获取结果: go run cmd/get-batch-results/main.go -token-file=%s -job-id=%s\n", 
		*tokenFile, *jobID)

	// 如果作业成功完成，建议获取结果
	if len(result.StateChanges) > 0 {
		lastState := result.StateChanges[len(result.StateChanges)-1].State
		if lastState == gemini.JobStateSucceeded {
			fmt.Printf("🎉 作业已成功完成，可以获取结果了!\n")
		}
	}
}