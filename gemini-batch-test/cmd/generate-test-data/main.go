package main

import (
	"flag"
	"fmt"
	"gemini-batch-test/internal/testdata"
	"gemini-batch-test/internal/utils"
	"log"
	"path/filepath"
)

func main() {
	var (
		count     = flag.Int("count", 10, "生成请求的数量")
		reqType   = flag.String("type", "simple", "请求类型 (simple, complex, mixed, advanced)")
		output    = flag.String("output", "", "输出文件名 (自动生成如果为空)")
		model     = flag.String("model", "gemini-2.5-flash", "使用的Gemini模型")
		outputDir = flag.String("dir", "output/test-data", "输出目录")
	)
	flag.Parse()

	fmt.Printf("🎯 生成测试数据配置:\n")
	fmt.Printf("   请求数量: %d\n", *count)
	fmt.Printf("   请求类型: %s\n", *reqType)
	fmt.Printf("   模型: %s\n", *model)
	fmt.Printf("   输出目录: %s\n", *outputDir)

	// 创建测试数据生成器
	generator := testdata.NewTestDataGenerator(*model)

	// 生成JSONL内容
	fmt.Printf("\n📝 正在生成测试数据...\n")
	jsonlContent, err := generator.GenerateJSONL(*reqType, *count)
	if err != nil {
		log.Fatalf("❌ 生成测试数据失败: %v", err)
	}

	// 确定输出文件名
	outputFile := *output
	if outputFile == "" {
		outputFile = fmt.Sprintf("test-%s-%d.jsonl", *reqType, *count)
	}
	outputPath := filepath.Join(*outputDir, outputFile)

	// 保存文件
	fmt.Printf("💾 正在保存到: %s\n", outputPath)
	err = utils.SaveJSONLContent(jsonlContent, outputPath)
	if err != nil {
		log.Fatalf("❌ 保存文件失败: %v", err)
	}

	fmt.Printf("✅ 测试数据生成成功!\n")
	fmt.Printf("   文件大小: %.2f KB\n", float64(len(jsonlContent))/1024)
	fmt.Printf("   文件路径: %s\n", outputPath)

	// 显示使用建议
	fmt.Printf("\n💡 下一步操作:\n")
	fmt.Printf("   创建批处理作业: go run cmd/create-batch-job/main.go -token-file=config/tokens/your-token.json -input=%s\n", outputPath)
}
