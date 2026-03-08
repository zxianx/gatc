package tool

import (
	"fmt"
	"os"
)

//  读取 LOCAL_PROXY_PORT 的本地代理端口，设置 取消设置代理,  本地test 用

var (
	oldHTTPProxy  string
	oldHTTPSProxy string
	oldAllProxy   string
)

//  注意http.Transport 默认读取代理， 自己创建 Transport 必须加
// transport := &http.Transport{
//    Proxy: http.ProxyFromEnvironment,
//}

func SetLocalProxy() error {
	port := os.Getenv("LOCAL_PROXY_PORT")
	fmt.Println("set local proxy port:", port, "skip: ", port == "")
	if port == "" {
		return fmt.Errorf("LOCAL_PROXY_PORT not set")
	}

	// 保存旧环境
	oldHTTPProxy = os.Getenv("HTTP_PROXY")
	oldHTTPSProxy = os.Getenv("HTTPS_PROXY")
	oldAllProxy = os.Getenv("ALL_PROXY")

	httpProxy := "http://127.0.0.1:" + port
	socksProxy := "socks5://127.0.0.1:" + port

	os.Setenv("HTTP_PROXY", httpProxy)
	os.Setenv("HTTPS_PROXY", httpProxy)
	os.Setenv("ALL_PROXY", socksProxy)

	return nil
}

func UnsetLocalProxy() {
	if oldHTTPProxy == "" {
		os.Unsetenv("HTTP_PROXY")
	} else {
		os.Setenv("HTTP_PROXY", oldHTTPProxy)
	}

	if oldHTTPSProxy == "" {
		os.Unsetenv("HTTPS_PROXY")
	} else {
		os.Setenv("HTTPS_PROXY", oldHTTPSProxy)
	}

	if oldAllProxy == "" {
		os.Unsetenv("ALL_PROXY")
	} else {
		os.Setenv("ALL_PROXY", oldAllProxy)
	}
}

func RunWithLocalProxy(fn func() error) error {
	if err := SetLocalProxy(); err != nil {
		return err
	}
	defer UnsetLocalProxy()

	return fn()
}
