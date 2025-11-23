package config

import (
	"encoding/json"
	"fmt"
	"gatc/base/zlog"
	"gatc/constants"
	"os"
	"sync"
)

// GCPConfig GCP相关的全局配置
type GCPConfig struct {
	ProjectID        string
	SSHKeyContent    string
	SSHPubKeyContent string
}

var (
	gcpConfig *GCPConfig
	once      sync.Once
)

// InitGCPConfig 初始化GCP配置，只执行一次
func InitGCPConfig() error {
	var initErr error
	once.Do(func() {
		gcpConfig = &GCPConfig{}
		
		// 读取服务账户密钥文件获取项目ID
		if err := gcpConfig.loadProjectID(); err != nil {
			initErr = fmt.Errorf("failed to load project ID: %v", err)
			return
		}
		
		// 读取SSH密钥内容
		if err := gcpConfig.loadSSHKeys(); err != nil {
			initErr = fmt.Errorf("failed to load SSH keys: %v", err)
			return
		}
		
		zlog.Info("GCP config initialized successfully", 
			"projectId", gcpConfig.ProjectID,
			"sshKeyLoaded", gcpConfig.SSHKeyContent != "",
			"pubKeyLoaded", gcpConfig.SSHPubKeyContent != "")
	})
	
	return initErr
}

// GetGCPConfig 获取GCP配置实例
func GetGCPConfig() *GCPConfig {
	return gcpConfig
}

// loadProjectID 从服务账户密钥文件加载项目ID
func (c *GCPConfig) loadProjectID() error {
	keyFile, err := os.ReadFile(constants.WhiteAccountKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read service account key file: %v", err)
	}
	
	var keyData struct {
		ProjectID string `json:"project_id"`
	}
	
	if err := json.Unmarshal(keyFile, &keyData); err != nil {
		return fmt.Errorf("failed to parse service account key file: %v", err)
	}
	
	if keyData.ProjectID == "" {
		return fmt.Errorf("project_id not found in service account key file")
	}
	
	c.ProjectID = keyData.ProjectID
	return nil
}

// loadSSHKeys 加载SSH密钥内容
func (c *GCPConfig) loadSSHKeys() error {
	// 读取私钥
	privateKeyBytes, err := os.ReadFile(constants.SSHKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read private key: %v", err)
	}
	c.SSHKeyContent = string(privateKeyBytes)
	
	// 读取公钥
	publicKeyBytes, err := os.ReadFile(constants.SSHPubKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read public key: %v", err)
	}
	c.SSHPubKeyContent = string(publicKeyBytes)
	
	return nil
}

// GetProjectID 获取项目ID
func (c *GCPConfig) GetProjectID() string {
	return c.ProjectID
}

// GetSSHKeyContent 获取SSH私钥内容
func (c *GCPConfig) GetSSHKeyContent() string {
	return c.SSHKeyContent
}

// GetSSHPubKeyContent 获取SSH公钥内容
func (c *GCPConfig) GetSSHPubKeyContent() string {
	return c.SSHPubKeyContent
}