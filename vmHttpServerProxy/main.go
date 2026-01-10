package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Port                int
	URLKeywordWhiteList []string
	DelHeaders          []string

	ResetHost   bool // true
	ClientReuse bool // true
	ForceHTTPS  bool // false
	Debug       bool // false
	AutoFollow  bool // true,
}

var Cfg Config

var (
	globalClient *http.Client
	clientOnce   sync.Once
)

// 获取全局HTTP客户端，使用连接池
func getHTTPClient() *http.Client {
	clientOnce.Do(func() {
		globalClient = &http.Client{
			Timeout: 150 * time.Second, // 适应30-120s的请求时间
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				// 连接池配置
				MaxIdleConns:        100,              // 最大空闲连接数
				MaxIdleConnsPerHost: 20,               // 每个host最大空闲连接
				MaxConnsPerHost:     50,               // 每个host最大连接数
				IdleConnTimeout:     90 * time.Second, // 空闲连接超时

				// TCP配置
				DisableKeepAlives:  false, // 启用keep-alive
				DisableCompression: false, // 启用压缩

				// 超时配置
				ResponseHeaderTimeout: 150 * time.Second, // 响应头超时
				ExpectContinueTimeout: 10 * time.Second,  // 100-continue超时
			},
		}
		if !Cfg.AutoFollow {
			globalClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		}
	})
	return globalClient
}

func getConnectToHTTPClientExt(connect_to string, sni string) *http.Client {
	// 有connect_to时，不使用连接池
	if connect_to != "" {
		client := &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				Proxy: nil,
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					// addr 是原始 host:port，但我们强制连 connectTo
					return net.DialTimeout(network, connect_to, 10*time.Second)
				},
				DisableKeepAlives: true,
				TLSClientConfig: &tls.Config{
					ServerName: sni,
				},
			},
		}
		if !Cfg.AutoFollow {
			client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		}
		return client
	}
	return getHTTPClient()
}

func initConfig() {
	Cfg = Config{
		Port:       1081,
		ForceHTTPS: false,
		Debug:      false,
	}

	if portStr := os.Getenv("HttpServerProxyPort"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			Cfg.Port = port
		}
	}

	if forceHTTPS := os.Getenv("force_https"); forceHTTPS == "true" {
		Cfg.ForceHTTPS = true
	}

	if debug := os.Getenv("debug"); debug == "true" {
		Cfg.Debug = true
	}

	Cfg.ResetHost = true
	if ResetHost := os.Getenv("reset_host"); ResetHost == "false" {
		Cfg.ResetHost = false
	}

	Cfg.ClientReuse = true
	if ClientReuse := os.Getenv("client_reuse"); ClientReuse == "false" {
		Cfg.ClientReuse = false
	}

	Cfg.AutoFollow = true
	if AutoFollow := os.Getenv("auto_follow"); AutoFollow == "false" {
		Cfg.AutoFollow = false
	}

	if whiteList := os.Getenv("proxy_url_keyword_white_list"); whiteList != "" {
		tmp := strings.Split(whiteList, "|")
		for _, s := range tmp {
			Cfg.URLKeywordWhiteList = append(Cfg.URLKeywordWhiteList, strings.ToLower(s))
		}
	}
	Cfg.URLKeywordWhiteList = append(Cfg.URLKeywordWhiteList, "ifconfig") //ifconfig.me 方便看代理是否生效

	if delHeaders := os.Getenv("proxy_del_headers"); delHeaders != "" {
		Cfg.DelHeaders = strings.Split(delHeaders, "|")
	}
	cf, _ := json.Marshal(Cfg)
	fmt.Println("Config: ", string(cf))
}

func (c *Config) isURLAllowed(targetURL string) bool {
	if len(c.URLKeywordWhiteList) == 0 {
		return true
	}

	for _, keyword := range c.URLKeywordWhiteList {
		if strings.Contains(strings.ToLower(targetURL), keyword) {
			return true
		}
	}
	return false
}

func (c *Config) removeHeaders(header http.Header) {
	for _, headerName := range c.DelHeaders {
		header.Del(headerName)
	}
}

func (c *Config) proxyHandler(w http.ResponseWriter, r *http.Request) {

	if c.Debug {
		fmt.Println("HEADER HOST", r.Host)
		for s, i := range r.Header {
			fmt.Printf("HEADER %s: %s\n", s, i[0])
		}
	}

	// 提取路径中的目标URL
	path := strings.TrimPrefix(r.RequestURI, "/px/")

	targetURL := path
	targetURL = strings.Replace(targetURL, "%3A%2F%2F", "://", 1)
	//fmt.Println(r.RequestURI)
	//fmt.Println(path)
	//fmt.Println(targetURL)

	// 检查是否是Gemini批处理请求
	if r.Header.Get("X-Gemini-Batch") == "1" && strings.Contains(targetURL, "v1beta/models/gemini") {
		c.handleBatchRequest(w, r, targetURL)
		return
	}

	// 强制HTTPS
	if c.ForceHTTPS && strings.HasPrefix(targetURL, "http://") {
		targetURL = strings.Replace(targetURL, "http://", "https://", 1)
	}

	// 检查URL白名单
	if !c.isURLAllowed(targetURL) {
		log.Printf("Blocked request to: %s", targetURL)
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// 解析目标URL
	target, err := url.Parse(targetURL)
	if err != nil {
		http.Error(w, "Invalid target URL", http.StatusBadRequest)
		return
	}

	// 创建新的请求
	req, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// 复制请求头
	for name, values := range r.Header {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	// 设置正确的Host头
	if c.ResetHost {
		req.Header.Set("Host", target.Host)
		req.Host = target.Host
	} else {
		req.Host = r.Host
		req.Header.Set("Host", r.Host)
	}

	// 删除指定的请求头
	c.removeHeaders(req.Header)

	// 不强制关闭连接，允许复用
	// req.Header.Set("Connection", "close")
	// req.Close = true

	var client *http.Client
	connectTo := r.Header.Get("X-Connect-To")
	if connectTo != "" {
		sni := target.Hostname()
		// fmt.Printf("Connect-To: %s\n", connectTo)
		client = getConnectToHTTPClientExt(connectTo, sni)
	} else if !c.ClientReuse {
		client = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy:                 http.ProxyFromEnvironment,
				DisableKeepAlives:     true,
				DisableCompression:    false,             // 启用压缩
				ResponseHeaderTimeout: 150 * time.Second, // 响应头超时
				ExpectContinueTimeout: 10 * time.Second,  // 100-continue超时
			},
		}
		if !c.AutoFollow {
			client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}
		}
	} else {
		client = getHTTPClient()
	}

	log.Printf("Proxying %s %s %s %s", r.Method, targetURL, req.Header.Get("Host"), req.Header.Get("User-Agent"))

	var remoteAddr string
	if c.Debug {
		trace := &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				remoteAddr = info.Conn.RemoteAddr().String()
			},
		}
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	}

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to proxy request to %s: %v", targetURL, err)
		http.Error(w, "Proxy request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 复制响应头
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}
	if c.Debug && remoteAddr != "" {
		w.Header().Set("X-Remote-Addr", remoteAddr)
	}

	// 设置状态码
	w.WriteHeader(resp.StatusCode)

	// 复制响应体
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("Failed to copy response body: %v", err)
	}
}

func main() {
	initConfig()
	config := Cfg

	log.Printf("Starting HTTP Proxy Server on port %d", config.Port)
	log.Printf("Force HTTPS: %v", config.ForceHTTPS)
	log.Printf("URL Whitelist: %v", config.URLKeywordWhiteList)
	log.Printf("Remove Headers: %v", config.DelHeaders)

	http.HandleFunc("/px/", config.proxyHandler)

	// 健康检查端点
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// 根路径说明
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "HTTP Proxy Server\n\nUsage: /px/{url}\nExample: /px/https://api.anthropic.com/v1/messages\n")
		} else {
			http.NotFound(w, r)
		}
	})

	addr := fmt.Sprintf(":%d", config.Port)
	log.Printf("Server listening on %s", addr)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("Failed to start server:", err)
	}
}

// 处理批处理请求
func (c *Config) handleBatchRequest(w http.ResponseWriter, r *http.Request, targetURL string) {
	// 统一使用Gemini批处理端点
	batchURL := "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:batchGenerateContent"

	// 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// 复制请求头并去除不需要的头
	header := r.Header.Clone()
	header.Del("X-Gemini-Batch")
	header.Del("Host") // Go会自动设置正确的Host头

	// 应用DelHeaders配置
	c.removeHeaders(header)

	// 添加到批处理管理器（使用统一的批处理端点）
	resultChan := batchManager.addToBatch(batchURL, header, body)

	//w.WriteHeader(200)
	//w.Write([]byte("debug , Request added to batch"))
	//return

	// 等待批处理结果
	result := <-resultChan

	if result.Error != nil {
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// 复制响应头（Go会自动处理Content-Length等）
	for name, values := range result.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	// 返回结果
	w.WriteHeader(result.StatusCode)
	w.Write(result.Body)
}
