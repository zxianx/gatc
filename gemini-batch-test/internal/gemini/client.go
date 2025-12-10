package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	BaseURL     = "https://generativelanguage.googleapis.com"
	FilesAPIURL = BaseURL + "/v1beta/files"
	BatchAPIURL = BaseURL + "/v1beta/models/gemini-2.5-flash:batchGenerateContent"
)

// Client Gemini API客户端
type Client struct {
	APIKey     string
	HTTPClient *http.Client
}

// NewClient 创建新的Gemini客户端
func NewClient(apiKey string) *Client {
	return &Client{
		APIKey: apiKey,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// UploadFile 上传JSONL文件到Google AI Files API
func (c *Client) UploadFile(ctx context.Context, filePath string, content []byte) (*UploadFileResponse, error) {
	// 创建multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// 添加文件字段
	fileWriter, err := writer.CreateFormFile("file", filePath)
	if err != nil {
		return nil, fmt.Errorf("创建文件字段失败: %w", err)
	}

	if _, err := fileWriter.Write(content); err != nil {
		return nil, fmt.Errorf("写入文件内容失败: %w", err)
	}

	// 添加metadata字段
	metadataWriter, err := writer.CreateFormField("metadata")
	if err != nil {
		return nil, fmt.Errorf("创建metadata字段失败: %w", err)
	}

	metadata := map[string]interface{}{
		"displayName": filePath,
	}
	metadataJSON, _ := json.Marshal(metadata)
	if _, err := metadataWriter.Write(metadataJSON); err != nil {
		return nil, fmt.Errorf("写入metadata失败: %w", err)
	}

	writer.Close()

	// 创建HTTP请求
	req, err := http.NewRequestWithContext(ctx, "POST", FilesAPIURL, &buf)
	if err != nil {
		return nil, fmt.Errorf("创建上传请求失败: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-Goog-Api-Key", c.APIKey)

	// 发送请求
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送上传请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取上传响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("文件上传失败 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	// 调试：打印原始响应
	fmt.Printf("📄 文件上传响应: %s\n", string(body))

	var uploadResp UploadFileResponse
	if err := json.Unmarshal(body, &uploadResp); err != nil {
		return nil, fmt.Errorf("解析上传响应失败: %w", err)
	}

	return &uploadResp, nil
}

// CreateBatchJob 创建批处理作业 - 使用内联请求格式
func (c *Client) CreateBatchJob(ctx context.Context, requests []BatchRequest) (*BatchJob, error) {
	// 转换为正确的格式
	var formattedRequests []map[string]interface{}
	for _, req := range requests {
		formattedReq := map[string]interface{}{
			"request": req.Request,
			"metadata": map[string]string{
				"key": req.Key,
			},
		}
		formattedRequests = append(formattedRequests, formattedReq)
	}

	payload := map[string]interface{}{
		"batch": map[string]interface{}{
			"display_name": fmt.Sprintf("batch-job-%d", time.Now().Unix()),
			"input_config": map[string]interface{}{
				"requests": map[string]interface{}{
					"requests": formattedRequests,
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("序列化请求数据失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", BatchAPIURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建批处理请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送批处理请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取批处理响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("创建批处理作业失败 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	var batchJob BatchJob
	if err := json.Unmarshal(body, &batchJob); err != nil {
		return nil, fmt.Errorf("解析批处理响应失败: %w", err)
	}

	return &batchJob, nil
}

// GetBatchJob 获取批处理作业状态
func (c *Client) GetBatchJob(ctx context.Context, jobName string) (*BatchJob, error) {
	url := fmt.Sprintf("%s/v1beta/%s", BaseURL, jobName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建状态查询请求失败: %w", err)
	}

	req.Header.Set("X-Goog-Api-Key", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送状态查询请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取状态查询响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询作业状态失败 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	// 调试：打印完整的状态查询响应
	fmt.Printf("📄 作业状态响应: %s\n", string(body))

	var batchJob BatchJob
	if err := json.Unmarshal(body, &batchJob); err != nil {
		return nil, fmt.Errorf("解析状态查询响应失败: %w", err)
	}

	return &batchJob, nil
}

// DownloadFile 下载文件内容
func (c *Client) DownloadFile(ctx context.Context, fileName string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/content", FilesAPIURL, fileName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建下载请求失败: %w", err)
	}

	req.Header.Set("X-Goog-Api-Key", c.APIKey)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送下载请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取下载响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载文件失败 (状态码: %d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}
