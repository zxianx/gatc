package constants

const (
	// GCP配置
	WhiteAccountKeyPath   = "./conf/gcp/sa-key0.json"
	DefaultZone           = "us-central1-a"
	DefaultMachineType    = "e2-small"
	MaxProjectsPerAccount = 12
	VMInitScriptPath      = "./scripts/vm_init.sh"

	// SSH密钥配置
	SSHKeyPath    = "./conf/gcp/gatc_rsa"
	SSHPubKeyPath = "./conf/gcp/gatc_rsa.pub"

	// VM状态
	VMStatusRunning = 1
	VMStatusStopped = 2
	VMStatusDeleted = 3
)
