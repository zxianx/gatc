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
		tokenFile = flag.String("token-file", "", "Token配置文件路径 (必需)")
		inputFile = flag.String("input", "", "输入JSONL文件路径 (必需)")
		saveState = flag.String("save-state", "", "保存作业状态的文件路径 (可选)")
	)
	flag.Parse()

	if *tokenFile == "" {
		log.Fatal("❌ 必须指定token配置文件: -token-file=config/tokens/your-token.json")
	}
	if *inputFile == "" {
		log.Fatal("❌ 必须指定输入JSONL文件: -input=output/test-data/test-simple-10.jsonl")
	}

	fmt.Printf("🚀 创建Gemini批处理作业\n")
	fmt.Printf("   Token配置: %s\n", *tokenFile)
	fmt.Printf("   输入文件: %s\n", *inputFile)

	// 加载Token配置
	fmt.Printf("\n🔑 正在加载Token配置...\n")
	config, err := utils.LoadTokenConfig(*tokenFile)
	if err != nil {
		log.Fatalf("❌ 加载Token配置失败: %v", err)
	}
	fmt.Printf("✅ Token配置加载成功: %s\n", config.Name)

	// 加载JSONL文件
	fmt.Printf("\n📁 正在加载JSONL文件...\n")
	jsonlContent, err := utils.LoadJSONLFile(*inputFile)
	if err != nil {
		log.Fatalf("❌ 加载JSONL文件失败: %v", err)
	}
	fmt.Printf("✅ JSONL文件加载成功: %.2f KB\n", float64(len(jsonlContent))/1024)

	// 创建批处理处理器
	processor := gemini.NewBatchProcessor(config.APIKey)

	// 上传文件并创建作业
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fileName := filepath.Base(*inputFile)
	batchJob, err := processor.CreateJobWithInlineRequests(ctx, jsonlContent, fileName)
	if err != nil {
		log.Fatalf("❌ 创建批处理作业失败: %v", err)
	}

	// 显示作业信息
	fmt.Printf("\n🎉 批处理作业创建成功!\n")
	fmt.Printf("   作业ID: %s\n", batchJob.Name)
	fmt.Printf("   模型: %s\n", batchJob.Metadata.Model)
	fmt.Printf("   显示名: %s\n", batchJob.Metadata.DisplayName)
	
	// 保存作业状态(可选)
	if *saveState != "" {
		if err := utils.SaveTestResult(&utils.TestResult{
			JobID:     batchJob.Name,
			StartTime: time.Now(),
		}, *saveState); err != nil {
			fmt.Printf("⚠️  保存作业状态失败: %v\n", err)
		} else {
			fmt.Printf("💾 作业状态已保存到: %s\n", *saveState)
		}
	}

	// 显示下一步操作
	fmt.Printf("\n💡 下一步操作:\n")
	fmt.Printf("   监控作业状态: go run cmd/monitor-batch-job/main.go -token-file=%s -job-id=%s\n", 
		*tokenFile, batchJob.Name)
	fmt.Printf("   或使用一键测试: go run cmd/full-test/main.go -token-file=%s -input=%s\n",
		*tokenFile, *inputFile)
}