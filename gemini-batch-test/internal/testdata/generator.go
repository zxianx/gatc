package testdata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"gemini-batch-test/internal/gemini"
)

// TestDataGenerator 测试数据生成器
type TestDataGenerator struct {
	model string
}

// NewTestDataGenerator 创建测试数据生成器
func NewTestDataGenerator(model string) *TestDataGenerator {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &TestDataGenerator{model: model}
}

// GenerateSimpleRequests 生成简单问答请求
func (g *TestDataGenerator) GenerateSimpleRequests(count int) []gemini.BatchRequest {
	simpleQuestions := []string{
		"什么是人工智能？",
		"请解释机器学习的基本概念。",
		"什么是深度学习？",
		"请介绍自然语言处理。",
		"什么是计算机视觉？",
		"请解释神经网络的工作原理。",
		"什么是大数据？",
		"请介绍云计算的概念。",
		"什么是物联网？",
		"请解释区块链技术。",
		"什么是量子计算？",
		"请介绍边缘计算。",
		"什么是数字孪生？",
		"请解释元宇宙概念。",
		"什么是增强现实？",
		"请介绍虚拟现实技术。",
		"什么是5G技术？",
		"请解释网络安全的重要性。",
		"什么是数据科学？",
		"请介绍算法的概念。",
	}

	requests := make([]gemini.BatchRequest, 0, count)
	for i := 0; i < count; i++ {
		question := simpleQuestions[i%len(simpleQuestions)]
		if i >= len(simpleQuestions) {
			question = fmt.Sprintf("%s (第%d次询问)", question, i/len(simpleQuestions)+1)
		}

		req := gemini.BatchRequest{
			Key: fmt.Sprintf("simple-request-%d", i+1),
			Request: gemini.RequestBody{
				Contents: []gemini.Content{
					{
						Parts: []gemini.Part{
							{Text: question},
						},
					},
				},
			},
		}
		requests = append(requests, req)
	}

	return requests
}

// GenerateAdvancedRequests 生成高级配置请求(类似用户示例)
func (g *TestDataGenerator) GenerateAdvancedRequests(count int) []gemini.BatchRequest {
	advancedTasks := []string{
		"你是一位专业严谨的初中数学老师，请解析这道几何题：已知射线OA的方向是北偏西66°，射线OB的方向是南偏东21°，求∠AOB的度数。请按照标准的数学解题格式输出。",
		"作为一名资深的物理教师，请详细解释牛顿第二定律的应用，并举出3个生活中的实际例子，要求包含具体的计算过程。",
		"你是一位语文老师，请分析《静夜思》这首诗的写作手法、意境营造和文学价值，并解释其在中国古典诗歌中的地位。",
		"作为化学专家，请详细解释水的电解实验过程，包括实验原理、步骤、现象观察和化学方程式，并分析其在化学教学中的重要性。",
		"你是一名历史老师，请分析明朝海禁政策的历史背景、具体措施、影响和历史评价，并联系当今开放政策进行对比分析。",
	}

	topP := 0.95
	temperature := 0.7
	maxTokens := 2048

	requests := make([]gemini.BatchRequest, 0, count)
	for i := 0; i < count; i++ {
		task := advancedTasks[i%len(advancedTasks)]
		if i >= len(advancedTasks) {
			task = fmt.Sprintf("%s\n\n请从第%d个不同角度进行深入分析。", task, i/len(advancedTasks)+1)
		}

		req := gemini.BatchRequest{
			Key: fmt.Sprintf("advanced-request-%d", i+1),
			Request: gemini.RequestBody{
				Contents: []gemini.Content{
					{
						Parts: []gemini.Part{
							{Text: task},
						},
						Role: "user",
					},
				},
				GenerationConfig: &gemini.GenerationConfig{
					TopP:            &topP,
					Temperature:     &temperature,
					MaxOutputTokens: &maxTokens,
					ThinkingConfig: &gemini.ThinkingConfig{
						IncludeThoughts: true,
						ThinkingBudget:  32768,
					},
				},
				SafetySettings: []gemini.SafetySetting{
					{Category: "HARM_CATEGORY_HARASSMENT", Threshold: "OFF"},
					{Category: "HARM_CATEGORY_HATE_SPEECH", Threshold: "OFF"},
					{Category: "HARM_CATEGORY_SEXUALLY_EXPLICIT", Threshold: "OFF"},
					{Category: "HARM_CATEGORY_DANGEROUS_CONTENT", Threshold: "OFF"},
					{Category: "HARM_CATEGORY_CIVIC_INTEGRITY", Threshold: "BLOCK_NONE"},
				},
				SystemInstruction: &gemini.SystemInstruction{
					Parts: []gemini.Part{
						{Text: "你是一位专业的教师和学者，请用专业、严谨、易懂的方式回答问题，确保内容准确、逻辑清晰。"},
					},
				},
			},
		}
		requests = append(requests, req)
	}

	return requests
}

// GenerateComplexRequests 生成复杂分析请求
func (g *TestDataGenerator) GenerateComplexRequests(count int) []gemini.BatchRequest {
	complexTasks := []string{
		"请详细分析人工智能在医疗领域的应用前景，包括诊断、治疗、药物研发等方面，并讨论可能面临的挑战和解决方案。",
		"分析大数据技术对现代商业模式的影响，包括数据驱动决策、个性化服务、预测分析等，并给出实施建议。",
		"探讨区块链技术在金融、供应链、数字身份等领域的应用案例，分析其优势、局限性和发展前景。",
		"详细分析云原生架构的优势和挑战，包括微服务、容器化、DevOps等方面的技术实现和最佳实践。",
		"研究物联网在智慧城市建设中的作用，分析传感器网络、数据处理、安全保障等关键技术要素。",
		"分析5G技术对各行业数字化转型的推动作用，包括制造业、教育、娱乐等领域的具体应用场景。",
		"探讨边缘计算与云计算的关系，分析在自动驾驶、工业4.0等场景下的技术选择策略。",
		"详细分析网络安全在数字化时代的重要性，包括威胁情报、零信任架构、安全运营等关键领域。",
		"研究量子计算对密码学、优化问题、机器学习等领域的潜在影响和应用前景。",
		"分析元宇宙概念下的技术架构，包括VR/AR、区块链、人工智能等技术的融合应用。",
	}

	requests := make([]gemini.BatchRequest, 0, count)
	for i := 0; i < count; i++ {
		task := complexTasks[i%len(complexTasks)]
		if i >= len(complexTasks) {
			task = fmt.Sprintf("%s\n\n请从第%d个角度进行深入分析。", task, i/len(complexTasks)+1)
		}

		req := gemini.BatchRequest{
			Key: fmt.Sprintf("complex-request-%d", i+1),
			Request: gemini.RequestBody{
				Contents: []gemini.Content{
					{
						Parts: []gemini.Part{
							{Text: task},
						},
					},
				},
			},
		}
		requests = append(requests, req)
	}

	return requests
}

// GenerateMixedRequests 生成混合类型请求
func (g *TestDataGenerator) GenerateMixedRequests(count int) []gemini.BatchRequest {
	requests := make([]gemini.BatchRequest, 0, count)

	// 50% 简单请求
	simpleCount := count / 2
	simpleReqs := g.GenerateSimpleRequests(simpleCount)
	requests = append(requests, simpleReqs...)

	// 50% 复杂请求
	complexCount := count - simpleCount
	complexReqs := g.GenerateComplexRequests(complexCount)
	requests = append(requests, complexReqs...)

	return requests
}

// GenerateJSONL 生成JSONL格式内容
func (g *TestDataGenerator) GenerateJSONL(requestType string, count int) ([]byte, error) {
	var requests []gemini.BatchRequest

	switch requestType {
	case "simple":
		requests = g.GenerateSimpleRequests(count)
	case "complex":
		requests = g.GenerateComplexRequests(count)
	case "mixed":
		requests = g.GenerateMixedRequests(count)
	case "advanced":
		requests = g.GenerateAdvancedRequests(count)
	default:
		return nil, fmt.Errorf("不支持的请求类型: %s，支持的类型: simple, complex, mixed, advanced", requestType)
	}

	var buf bytes.Buffer
	for _, req := range requests {
		jsonData, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("序列化请求失败: %w", err)
		}
		buf.Write(jsonData)
		buf.WriteString("\n")
	}

	return buf.Bytes(), nil
}

// GenerateTestConfig 生成测试配置
func (g *TestDataGenerator) GenerateTestConfig(requestType string, count int) (*gemini.TestConfig, error) {
	var requests []gemini.BatchRequest

	switch requestType {
	case "simple":
		requests = g.GenerateSimpleRequests(count)
	case "complex":
		requests = g.GenerateComplexRequests(count)
	case "mixed":
		requests = g.GenerateMixedRequests(count)
	case "advanced":
		requests = g.GenerateAdvancedRequests(count)
	default:
		return nil, fmt.Errorf("不支持的请求类型: %s", requestType)
	}

	return &gemini.TestConfig{
		RequestCount: count,
		RequestType:  requestType,
		Model:        g.model,
		TestName:     fmt.Sprintf("gemini-batch-test-%s-%d", requestType, count),
		Requests:     requests,
	}, nil
}
