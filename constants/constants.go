package constants

const (
	// GCP配置
	WhiteAccountKeyPath       = "./conf/gcp/sa-key0.json"
	DefaultZone               = "us-central1-a"
	DefaultMachineType        = "e2-small"
	MaxProjectsPerAccount     = 12
	VMInitScriptPath          = "./scripts/vm_init.sh"
	VMInitScriptTinyProxyPath = "./scripts/vm_init_tinyproxy.sh"
	VMInitScriptHttpProxyPath = "./scripts/vm_init_httpproxy.sh"

	// SSH密钥配置
	SSHKeyPath    = "./conf/gcp/gatc_rsa"
	SSHPubKeyPath = "./conf/gcp/gatc_rsa.pub"

	// VM状态
	VMStatusRunning       = 1
	VMStatusStopped       = 2
	VMStatusDeleted       = 3
	VMStatusPendingDelete = 4 // 预删除状态

	// VM预删除状态保留时间（小时）
	VMPendingDeleteRetentionHours = 1

	// 代理类型
	ProxyTypeSocks5         = "socks5"
	ProxyTypeTinyProxy      = "tinyproxy"
	ProxyTypeHttpProxy      = "httpProxyServer"
	ProxyTypeHttpProxyAlias = "server"
)
