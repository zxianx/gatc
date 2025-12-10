package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// 批处理请求结构
type BatchRequest struct {
	Key     string      `json:"key"`
	Body    []byte      `json:"-"`
	Request interface{} `json:"request"`
}

// 批处理结果
type BatchResult struct {
	Key        string
	StatusCode int
	Header     http.Header
	Body       []byte
	Error      error
}

// 批处理收集器
type BatchCollector struct {
	requests    []BatchRequest
	responseChs []chan BatchResult
	firstHeader http.Header
	targetURL   string
	timer       *time.Timer
	mu          sync.Mutex
	batchID     int64 // 批次ID
}

// 批处理管理器
type BatchManager struct {
	batches map[string]*BatchCollector
	mu      sync.RWMutex
}

// 批处理专用HTTP客户端
var (
	batchClient *http.Client
	batchOnce   sync.Once
)

// 获取批处理专用HTTP客户端，用于异步提交和轮询
func getBatchHTTPClient() *http.Client {
	batchOnce.Do(func() {
		batchClient = &http.Client{
			Timeout: 60 * time.Second, // 仅用于提交和轮询，不需要太长
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 10,
				MaxConnsPerHost:     20,
				IdleConnTimeout:     1 * time.Minute,

				DisableKeepAlives:  false,
				DisableCompression: true, // 禁用自动压缩，避免编码问题

				ResponseHeaderTimeout: 60 * time.Second, // 提交和查询响应快
				ExpectContinueTimeout: 10 * time.Second,
			},
		}
	})
	return batchClient
}

// 解压gzip响应体
func decompressGzip(data []byte) ([]byte, error) {
	// 检查是否是gzip格式 (魔数: 1f 8b)
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil // 不是gzip，直接返回原数据
	}

	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// 全局批处理管理器
var batchManager = &BatchManager{
	batches: make(map[string]*BatchCollector),
}

var batchAutoIncId int64
var batchGlobalID int64 // 全局批次号

// 添加请求到批处理
func (bm *BatchManager) addToBatch(targetURL string, header http.Header, body []byte) chan BatchResult {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// 使用targetURL作为批次key
	batchKey := targetURL

	collector, exists := bm.batches[batchKey]
	if !exists {
		// 分配批次ID
		batchID := atomic.AddInt64(&batchGlobalID, 1)

		// 创建新的批处理收集器
		collector = &BatchCollector{
			requests:    make([]BatchRequest, 0, BatchMaxSize),
			responseChs: make([]chan BatchResult, 0, BatchMaxSize),
			firstHeader: header.Clone(),
			targetURL:   targetURL,
			batchID:     batchID,
		}
		bm.batches[batchKey] = collector

		log.Printf("[Batch %d] 创建新批次", batchID)

		// 设置定时器
		collector.timer = time.AfterFunc(BatchCollectTimeout, func() {
			collector.executeBatch()
			bm.removeBatch(batchKey)
		})
	}

	collector.mu.Lock()
	defer collector.mu.Unlock()

	// 创建请求和响应通道，使用批次ID和索引
	idx := len(collector.requests)
	requestKey := fmt.Sprintf("req_b_%d_i_%d", collector.batchID, idx)
	resultChan := make(chan BatchResult, 1)

	// 解析body为JSON（假设是JSON请求）
	var requestData interface{}
	json.Unmarshal(body, &requestData)

	batchReq := BatchRequest{
		Key:     requestKey,
		Body:    body,
		Request: requestData,
	}

	collector.requests = append(collector.requests, batchReq)
	collector.responseChs = append(collector.responseChs, resultChan)

	log.Printf("[Batch %d] 添加请求: %s, 当前数量: %d/%d", collector.batchID, requestKey, len(collector.requests), BatchMaxSize)

	// 检查是否达到批次大小
	if len(collector.requests) >= BatchMaxSize {
		collector.timer.Stop()
		go func() {
			collector.executeBatch()
			bm.removeBatch(batchKey)
		}()
	}

	return resultChan
}

// 移除批处理收集器
func (bm *BatchManager) removeBatch(batchKey string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	delete(bm.batches, batchKey)
}

// 执行批处理
func (bc *BatchCollector) executeBatch() {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if len(bc.requests) == 0 {
		return
	}

	reqCount := len(bc.requests)
	log.Printf("[Batch %d] 执行批处理,请求数量: %d:url %s, ", bc.batchID, reqCount, bc.targetURL)

	// 动态超时: 3min + 个数*30s (用于轮询总超时)
	dynamicTimeout := 3*time.Minute + time.Duration(reqCount)*30*time.Second
	pollingDeadline := time.Now().Add(dynamicTimeout)

	// 构造Gemini批处理格式
	var formattedRequests []map[string]interface{}
	for _, req := range bc.requests {
		formattedReq := map[string]interface{}{
			"request":  req.Request,
			"metadata": map[string]string{"key": req.Key},
		}
		formattedRequests = append(formattedRequests, formattedReq)
	}

	payload := map[string]interface{}{
		"batch": map[string]interface{}{
			"display_name": fmt.Sprintf("batch-%d", time.Now().Unix()),
			"input_config": map[string]interface{}{
				"requests": map[string]interface{}{
					"requests": formattedRequests,
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		bc.sendErrorStatusToAll(500, "Marshal batch payload failed")
		return
	}

	if Debug {
		log.Printf("[Batch %d] payload %s", bc.batchID, string(jsonData))
	}

	// 创建批处理请求（不使用context超时，因为提交很快）
	batchReq, err := http.NewRequest("POST", bc.targetURL, bytes.NewReader(jsonData))
	if err != nil {
		bc.sendErrorToAll(fmt.Errorf("创建批处理请求失败: %v", err))
		return
	}

	// 设置请求头（使用首个请求的头，DelHeaders已在传入时处理）
	batchReq.Header = bc.firstHeader.Clone()
	batchReq.Header.Set("Content-Type", "application/json")

	// 删除Go会自动处理的请求头
	batchReq.Header.Del("Connection")
	batchReq.Header.Del("Content-Length")

	// 步骤1: 提交批处理作业
	client := getBatchHTTPClient()
	startTime := time.Now()
	resp, err := client.Do(batchReq)
	if err != nil {
		log.Printf("[Batch %d] 提交批处理作业失败: %v", bc.batchID, err)
		bc.sendErrorStatusToAll(429, "Too Many Requests")
		return
	}
	defer resp.Body.Close()

	createBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[Batch %d] 读取作业创建响应失败: %v", bc.batchID, err)
		bc.sendErrorStatusToAll(429, "Too Many Requests")
		return
	}

	// 解压gzip响应（如果需要）
	createBody, err = decompressGzip(createBody)
	if err != nil {
		log.Printf("[Batch %d] 解压响应失败: %v", bc.batchID, err)
		bc.sendErrorStatusToAll(500, "Decompress response failed")
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Batch %d] 创建批处理作业失败: 状态码=%d, 响应=%s", bc.batchID, resp.StatusCode, string(createBody))
		bc.sendErrorStatusToAll(resp.StatusCode, string(createBody))
		return
	}

	// 解析批处理作业响应，获取batch名称
	var createResp map[string]interface{}
	if err := json.Unmarshal(createBody, &createResp); err != nil {
		log.Printf("[Batch %d] 解析作业创建响应失败: %v, Content-Type=%s, Content-Encoding=%s, 响应前100字节=%x",
			bc.batchID, err, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Encoding"), createBody[:min(100, len(createBody))])
		bc.sendErrorStatusToAll(500, "Parse create response failed")
		return
	}

	batchName, ok := createResp["name"].(string)
	if !ok || batchName == "" {
		log.Printf("[Batch %d] 未找到批处理作业名称", bc.batchID)
		bc.sendErrorStatusToAll(500, "Batch name not found")
		return
	}

	log.Printf("[Batch %d] 批处理作业已创建: %s", bc.batchID, batchName)

	// 步骤2: 轮询作业状态直到完成
	time.Sleep(50 * time.Second)
	pollingInterval := 10 * time.Second
	for {
		// 检查轮询超时
		if time.Now().After(pollingDeadline) {
			log.Printf("[Batch %d] 批处理轮询超时", bc.batchID)
			bc.sendErrorStatusToAll(504, "Gateway Timeout")
			return
		}

		time.Sleep(pollingInterval)

		// 构造状态查询URL
		statusURL := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/%s", batchName)
		statusReq, err := http.NewRequest("GET", statusURL, nil)
		if err != nil {
			log.Printf("[Batch %d] 创建状态查询请求失败: %v", bc.batchID, err)
			bc.sendErrorStatusToAll(500, "Create status request failed")
			return
		}

		// 复制认证头
		for k, v := range bc.firstHeader {
			if k == "X-Goog-Api-Key" || k == "Authorization" {
				statusReq.Header[k] = v
			}
		}

		statusResp, err := client.Do(statusReq)
		if err != nil {
			log.Printf("[Batch %d] 查询作业状态失败: %v", bc.batchID, err)
			continue
		}

		statusBody, err := io.ReadAll(statusResp.Body)
		statusResp.Body.Close()
		if err != nil {
			log.Printf("[Batch %d] 读取状态响应失败: %v", bc.batchID, err)
			continue
		}

		// 解压gzip响应（如果需要）
		statusBody, err = decompressGzip(statusBody)
		if err != nil {
			log.Printf("[Batch %d] 解压状态响应失败: %v", bc.batchID, err)
			continue
		}

		var statusData map[string]interface{}
		if err := json.Unmarshal(statusBody, &statusData); err != nil {
			log.Printf("[Batch %d] 解析状态响应失败: %v", bc.batchID, err)
			continue
		}

		// 检查作业状态
		metadata, ok := statusData["metadata"].(map[string]interface{})
		if !ok {
			log.Printf("[Batch %d] 状态响应缺少metadata字段", bc.batchID)
			continue
		}

		state, _ := metadata["state"].(string)
		log.Printf("[Batch %d] 批处理作业状态: %s", bc.batchID, state)

		if state == "BATCH_STATE_SUCCEEDED" {
			// 作业完成，提取结果
			elapsed := time.Since(startTime)
			log.Printf("[Batch %d] 批处理作业完成: 耗时=%.2fs", bc.batchID, elapsed.Seconds())
			bc.extractAndDistributeResults(metadata, statusResp.Header)
			return
		} else if state == "BATCH_STATE_FAILED" || state == "BATCH_STATE_CANCELLED" {
			log.Printf("[Batch %d] 批处理作业失败: state=%s", bc.batchID, state)
			bc.sendErrorStatusToAll(500, "Batch job failed")
			return
		}
		// 继续轮询
	}
}

// 发送错误给所有等待的请求
func (bc *BatchCollector) sendErrorToAll(err error) {
	for _, ch := range bc.responseChs {
		select {
		case ch <- BatchResult{Error: err}:
		default:
		}
	}
}

// 发送错误状态码给所有等待的请求
func (bc *BatchCollector) sendErrorStatusToAll(statusCode int, statusText string) {
	for _, ch := range bc.responseChs {
		select {
		case ch <- BatchResult{
			StatusCode: statusCode,
			Header:     make(http.Header),
			Body:       []byte(statusText),
		}:
		default:
		}
	}
}

// 从metadata中提取结果并分发
func (bc *BatchCollector) extractAndDistributeResults(metadata map[string]interface{}, respHeader http.Header) {
	// 提取output字段
	output, ok := metadata["output"].(map[string]interface{})
	if !ok {
		log.Printf("[Batch %d] metadata中缺少output字段", bc.batchID)
		bc.sendErrorStatusToAll(500, "No output in metadata")
		return
	}

	inlinedResponses, ok := output["inlinedResponses"].(map[string]interface{})
	if !ok {
		log.Printf("[Batch %d] output中缺少inlinedResponses字段", bc.batchID)
		bc.sendErrorStatusToAll(500, "No inlinedResponses")
		return
	}

	responses, ok := inlinedResponses["inlinedResponses"].([]interface{})
	if !ok {
		log.Printf("[Batch %d] inlinedResponses格式错误", bc.batchID)
		bc.sendErrorStatusToAll(500, "Invalid inlinedResponses format")
		return
	}

	// 复制响应头并删除Go会自动处理的头
	baseHeader := respHeader.Clone()
	baseHeader.Del("Content-Length")
	baseHeader.Del("Transfer-Encoding")
	baseHeader.Del("Connection")

	// 按key分发结果
	keyToResponse := make(map[string][]byte)
	for _, resp := range responses {
		respMap, ok := resp.(map[string]interface{})
		if !ok {
			continue
		}

		// 提取metadata.key
		respMetadata, ok := respMap["metadata"].(map[string]interface{})
		if !ok {
			continue
		}

		key, ok := respMetadata["key"].(string)
		if !ok {
			continue
		}

		// 序列化整个响应为JSON
		respJSON, err := json.Marshal(respMap["response"])
		if err != nil {
			log.Printf("[Batch %d] 序列化响应失败: key=%s, err=%v", bc.batchID, key, err)
			continue
		}

		keyToResponse[key] = respJSON
	}

	log.Printf("[Batch %d] 成功提取%d个响应结果", bc.batchID, len(keyToResponse))

	// 按原请求顺序分发结果
	for i, req := range bc.requests {
		if i >= len(bc.responseChs) {
			break
		}

		responseBody, found := keyToResponse[req.Key]
		if !found {
			log.Printf("[Batch %d] 响应缺失: %s", bc.batchID, req.Key)
			responseBody = []byte(fmt.Sprintf(`{"error":"Response not found for key: %s"}`, req.Key))
		}

		select {
		case bc.responseChs[i] <- BatchResult{
			Key:        req.Key,
			StatusCode: 200,
			Header:     baseHeader.Clone(),
			Body:       responseBody,
		}:
		default:
		}
	}
}

// 分发批处理响应
func (bc *BatchCollector) distributeBatchResponse(statusCode int, respHeader http.Header, respBody []byte) {
	// 复制响应头并删除Go会自动处理的头
	baseHeader := respHeader.Clone()
	baseHeader.Del("Content-Length") // Go会自动计算
	//baseHeader.Del("Content-Type")   // 每个响应可能不同，让Go自动处理
	baseHeader.Del("Transfer-Encoding")
	baseHeader.Del("Connection")

	// 如果是成功的批处理响应，尝试按行解析
	if statusCode == 200 {
		lines := strings.Split(string(respBody), "\n")

		for i, ch := range bc.responseChs {
			var resultBody []byte

			if i < len(lines) && strings.TrimSpace(lines[i]) != "" {
				// 使用对应行的响应
				resultBody = []byte(lines[i])
			} else {
				// 使用整个响应体
				resultBody = respBody
			}

			select {
			case ch <- BatchResult{
				Key:        bc.requests[i].Key,
				StatusCode: statusCode,
				Header:     baseHeader.Clone(), // 每个请求独立的头副本
				Body:       resultBody,
			}:
			default:
			}
		}
	} else {
		// 错误响应，发送给所有请求
		for _, ch := range bc.responseChs {
			select {
			case ch <- BatchResult{
				StatusCode: statusCode,
				Header:     baseHeader.Clone(),
				Body:       respBody,
			}:
			default:
			}
		}
	}
}
