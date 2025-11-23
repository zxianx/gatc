package env

import (
	"os"
	"strings"
)

var (
	// Hostname 当前主机名，程序启动时获取一次
	Hostname    string
	DevLocalEnv bool
)

// Init 初始化环境变量，获取主机名
func Init() error {
	tmp, err := os.Hostname()
	if err != nil {
		return err
	}
	Hostname = tmp
	// 移除可能的域名后缀，只保留主机名部分
	// Hostname = strings.Split(hostname, ".")[0]

	host_l := strings.ToLower(Hostname)
	DevLocalEnv = strings.Contains(host_l, "macbook") || strings.Contains(host_l, "local")
	return nil
}

// GetHostname 获取主机名
func GetHostname() string {
	return Hostname
}
