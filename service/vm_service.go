package service

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"gatc/base/config"
	"gatc/base/zlog"
	"gatc/constants"
	"gatc/dao"
	"gatc/tool"
	mrand "math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// CreateVMParam 创建VM输入参数
type CreateVMParam struct {
	Zone        string `json:"zone,omitempty"`
	MachineType string `json:"machine_type,omitempty"`
	Tag         string `json:"tag,omitempty"`
	ProxyType   string `json:"proxy_type,omitempty"` // 代理类型：socks5(默认)/tinyproxy  //
}

// CreateVMResult 创建VM返回结果
type CreateVMResult struct {
	VMID          string `json:"vm_id"`
	VMName        string `json:"vm_name"`
	ExternalIP    string `json:"external_ip"`
	Proxy         string `json:"proxy"`
	SSHConnection string `json:"ssh_connection"`
	Msg           string `json:"msg"`
}

// DeleteVMParam 删除VM输入参数
type DeleteVMParam struct {
	VMID string `json:"vm_id"`
}

// DeleteVMResult 删除VM返回结果
type DeleteVMResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	VMID    string `json:"vm_id"`
}

// ListVMParam 查询VM列表输入参数
type ListVMParam struct {
	Status int `json:"status,omitempty" form:"status"`
	Page   int `json:"page,omitempty" form:"page"`
	Size   int `json:"size,omitempty" form:"size"`
}

// ListVMResult 查询VM列表返回结果
type ListVMResult struct {
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Size  int              `json:"size"`
	Items []dao.VMInstance `json:"items"`
}

// GetVMParam 查询单个VM输入参数
type GetVMParam struct {
	VMID string `json:"vm_id" form:"vm_id"`
}

// GetVMResult 查询单个VM返回结果
type GetVMResult struct {
	*dao.VMInstance
}

// RefreshVMIPParam 刷新VM外网IP参数
type RefreshVMIPParam struct {
	VMID string `json:"vm_id"`
}

// RefreshVMIPResult 刷新VM外网IP结果
type RefreshVMIPResult struct {
	VMID       string `json:"vm_id"`
	ExternalIP string `json:"external_ip"`
	Updated    bool   `json:"updated"`
}

type BatchCreateVMParam struct {
	Num         int    `json:"num"`
	Zone        string `json:"zone,omitempty"`
	MachineType string `json:"machine_type,omitempty"`
	Tag         string `json:"tag,omitempty"`
	ProxyType   string `json:"proxy_type,omitempty"`
}

type BatchCreateVMResult struct {
	Total   int              `json:"total"`
	Success int              `json:"success"`
	Failed  int              `json:"failed"`
	Results []CreateVMResult `json:"results"`
}

type BatchDeleteVMParam struct {
	VMList []string `json:"vm_list,omitempty"`
	Prefix string   `json:"prefix,omitempty"` // 优先vm_list参数，其次prefix
	Limit  int      `json:"limit,omitempty"`  //只 针对prefix 参数有效
}

type BatchDeleteVMResult struct {
	Total   int              `json:"total"`
	Success int              `json:"success"`
	Failed  int              `json:"failed"`
	Results []DeleteVMResult `json:"results"`
}

type VMService struct{}

var GVmService = &VMService{}

// 防止并发执行的标志位
var cleanupRunning atomic.Bool

// isGATCVM 检查VM名称是否是GATC创建的VM
func (s *VMService) isGATCVM(vmName string) bool {
	return strings.HasPrefix(vmName, "gatc-vm") || strings.HasPrefix(vmName, "gatcvm")
}

// CleanupOldVMs 清理24小时前创建的VM
func (s *VMService) CleanupOldVMs() {
	// 防止并发执行
	if !cleanupRunning.CompareAndSwap(false, true) {
		zlog.Info("CleanupOldVMs VM cleanup already running, skipping this execution")
		return
	}
	defer cleanupRunning.Store(false)

	c := &gin.Context{}

	// 每小时清理24小时前的VM
	existH := os.Getenv("CLEAN_OLD_VM_EXIST_EXCEED_H")
	if existH == "" {
		zlog.InfoWithCtx(c, "CleanupOldVMs SKIP. Starting cleanup of old VMs")
		return
	}
	h, err2 := strconv.Atoi(existH)
	if err2 != nil {
		zlog.Error("CleanupOldVMs ", "illegal CLEAN_OLD_VM_EXIST_EXCEED_H, val:", existH)
		return
	}

	zlog.InfoWithCtx(c, "CleanupOldVMs Starting cleanup of old VMs, ", h)

	// 获取24小时前创建的VM
	cutoffTime := time.Now().Add(-time.Duration(h) * time.Hour)

	vms, err := dao.GVmInstanceDao.GetVMsCreatedBefore(c, cutoffTime)
	if err != nil {
		zlog.ErrorWithCtx(c, "CleanupOldVMs Failed to get old VMs", err)
		return
	}

	if len(vms) == 0 {
		zlog.InfoWithCtx(c, "CleanupOldVMs No old VMs to cleanup")
		return
	}

	successCount := 0
	for _, vm := range vms {
		// 只处理GATC创建的VM
		if !s.isGATCVM(vm.VMID) {
			zlog.InfoWithCtx(c, "CleanupOldVMs Skipping non-GATC VM during cleanup", "vmId", vm.VMID)
			continue
		}

		// 删除GCP中的VM实例
		if err := s.deleteVMFromGCP(c, &vm); err != nil {
			zlog.ErrorWithCtx(c, "CleanupOldVMs Failed to delete VM from GCP", err)
			continue
		}

		// 更新数据库状态
		if err := dao.GVmInstanceDao.UpdateStatus(c, vm.VMID, constants.VMStatusDeleted); err != nil {
			zlog.ErrorWithCtx(c, "CleanupOldVMs Failed to update VM status", err)
			continue
		}

		successCount++
		zlog.InfoWithCtx(c, "CleanupOldVMs Deleted old VM", "vmId", vm.VMID)
	}

	zlog.InfoWithCtx(c, "CleanupOldVMs Cleanup of old VMs completed", "processed", successCount)
}

// deleteVMFromGCP 从GCP中删除VM实例
func (s *VMService) deleteVMFromGCP(c *gin.Context, vm *dao.VMInstance) error {
	// 激活服务账户
	if err := s.activateServiceAccount(c); err != nil {
		return fmt.Errorf("failed to activate service account: %v", err)
	}

	gcpConfig := config.GetGCPConfig()
	projectID := gcpConfig.GetProjectID()

	cmdStr := fmt.Sprintf("gcloud compute instances delete %s "+
		"--project=%s --zone=%s --quiet", vm.VMID, projectID, vm.Zone)

	zlog.InfoWithCtx(c, "Deleting VM from GCP", "command", cmdStr)

	stdout, stderr, _, err := tool.ExecCommand(cmdStr)
	if err != nil {
		// 检查是否是因为VM已经不存在导致的错误
		if strings.Contains(stderr, "was not found") || strings.Contains(stderr, "not found") {
			zlog.InfoWithCtx(c, "VM already deleted from GCP", "vm_id", vm.VMID)
			return nil
		}
		zlog.ErrorWithCtx(c, "Failed to delete VM from GCP", err)
		return err
	}

	zlog.InfoWithCtx(c, "VM deleted from GCP successfully", "stdout", stdout)
	return nil
}

// generateRandomString 生成指定长度的随机字符串
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[mrand.Intn(len(charset))]
	}
	return string(b)
}

// generateProxyCredentials 生成SOCKS5代理的用户名和密码
func generateProxyCredentials() (string, string) {
	username := "gatc" + generateRandomString(6)
	password := generateRandomString(12)
	return username, password
}

// validateVMTag 验证VM标签是否符合GCP命名规范
func validateVMTag(tag string) error {
	if tag == "" {
		return nil
	}

	// GCP VM名称规范：
	// 1. 只能包含小写字母、数字和连字符
	// 2. 必须以小写字母开头
	// 3. 不能以连字符结尾
	// 4. 长度1-63个字符

	if len(tag) > 50 { // 预留空间给vm前缀和时间戳
		return fmt.Errorf("tag长度不能超过50个字符，当前长度：%d", len(tag))
	}

	// 检查是否只包含允许的字符
	for _, r := range tag {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("tag只能包含小写字母、数字和连字符，发现无效字符：%c", r)
		}
	}

	// 检查是否以小写字母开头
	if tag[0] < 'a' || tag[0] > 'z' {
		return fmt.Errorf("tag必须以小写字母开头")
	}

	// 检查是否以连字符结尾
	if tag[len(tag)-1] == '-' {
		return fmt.Errorf("tag不能以连字符结尾")
	}

	return nil
}

func (s *VMService) EnsureSSHKeys() error {
	privateKeyPath := constants.SSHKeyPath
	publicKeyPath := constants.SSHPubKeyPath

	if _, err := os.Stat(privateKeyPath); os.IsNotExist(err) {
		zlog.Info("SSH key not found, generating new key pair")
		return s.generateSSHKeyPair(privateKeyPath, publicKeyPath)
	}

	zlog.Info("SSH key pair already exists")
	return nil
}

func (s *VMService) generateSSHKeyPair(privateKeyPath, publicKeyPath string) error {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("failed to generate private key: %v", err)
	}

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}

	os.MkdirAll(filepath.Dir(privateKeyPath), 0700)

	privateFile, err := os.Create(privateKeyPath)
	if err != nil {
		return fmt.Errorf("failed to create private key file: %v", err)
	}
	defer privateFile.Close()

	if err := pem.Encode(privateFile, privateKeyPEM); err != nil {
		return fmt.Errorf("failed to write private key: %v", err)
	}

	if err := os.Chmod(privateKeyPath, 0600); err != nil {
		return fmt.Errorf("failed to set private key permissions: %v", err)
	}

	publicRSAKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return fmt.Errorf("failed to generate public key: %v", err)
	}

	publicKeyBytes := ssh.MarshalAuthorizedKey(publicRSAKey)

	publicFile, err := os.Create(publicKeyPath)
	if err != nil {
		return fmt.Errorf("failed to create public key file: %v", err)
	}
	defer publicFile.Close()

	if _, err := publicFile.Write(publicKeyBytes); err != nil {
		return fmt.Errorf("failed to write public key: %v", err)
	}

	zlog.Info("SSH key pair generated successfully")
	return nil
}

func (s *VMService) activateServiceAccount(c *gin.Context) error {
	cmdStr := fmt.Sprintf("gcloud auth activate-service-account --key-file=%s", constants.WhiteAccountKeyPath)

	zlog.InfoWithCtx(c, "Activating service account", "command", cmdStr)

	stdout, stderr, _, err := tool.ExecCommand(cmdStr)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to activate service account", err)
		return fmt.Errorf("failed to activate service account: %v, stderr: %s", err, stderr)
	}

	zlog.InfoWithCtx(c, "Service account activated successfully", "stdout", stdout)
	return nil
}

func (s *VMService) CreateVM(c *gin.Context, param *CreateVMParam) (*CreateVMResult, error) {
	// 验证tag参数
	if err := validateVMTag(param.Tag); err != nil {
		zlog.ErrorWithCtx(c, "VM tag validation failed", err)
		return nil, fmt.Errorf("标签验证失败: %v", err)
	}

	zone := constants.DefaultZone
	if param.Zone != "" {
		zone = param.Zone
	}

	machineType := constants.DefaultMachineType
	if param.MachineType != "" {
		machineType = param.MachineType
	}

	// 处理代理类型，默认为socks5
	proxyType := constants.ProxyTypeSocks5
	if param.ProxyType != "" {
		if param.ProxyType == constants.ProxyTypeTinyProxy {
			proxyType = constants.ProxyTypeTinyProxy
		} else if param.ProxyType == constants.ProxyTypeHttpProxy || param.ProxyType == constants.ProxyTypeHttpProxyAlias {
			proxyType = constants.ProxyTypeHttpProxy
		}
	}

	if err := s.EnsureSSHKeys(); err != nil {
		return nil, fmt.Errorf("failed to ensure SSH keys: %v", err)
	}

	// 激活服务账户
	if err := s.activateServiceAccount(c); err != nil {
		return nil, fmt.Errorf("failed to activate service account: %v", err)
	}

	// 从全局配置获取项目ID和SSH密钥
	gcpConfig := config.GetGCPConfig()
	if gcpConfig == nil {
		return nil, fmt.Errorf("GCP config not initialized")
	}

	projectID := gcpConfig.GetProjectID()
	vmName := fmt.Sprintf("gatcvm-%s-%s-%s", strings.ToLower(proxyType), strings.ToLower(param.Tag), time.Now().Format("0102150405"))

	// 生成代理的用户名和密码
	proxyUsername, proxyPassword := generateProxyCredentials()

	// 根据代理类型选择初始化脚本
	var initScriptPath string
	if proxyType == constants.ProxyTypeTinyProxy {
		initScriptPath = constants.VMInitScriptTinyProxyPath
	} else if proxyType == constants.ProxyTypeHttpProxy {
		initScriptPath = constants.VMInitScriptHttpProxyPath
	} else {
		initScriptPath = constants.VMInitScriptPath
	}

	// 使用SSH公钥作为metadata
	sshKeyMetadata := fmt.Sprintf("gatc:%s", strings.TrimSpace(gcpConfig.GetSSHPubKeyContent()))

	cmdStr := fmt.Sprintf("gcloud compute instances create %s "+
		"--project=%s --zone=%s --machine-type=%s --network-tier=STANDARD --maintenance-policy=MIGRATE "+
		"--image-family=debian-12 --image-project=debian-cloud "+
		"--boot-disk-type=pd-standard "+
		"--metadata=ssh-keys='%s',proxy-username='%s',proxy-password='%s' "+
		"--metadata-from-file=startup-script=%s "+
		"--tags=http-server,https-server --format=json",
		vmName, projectID, zone, machineType, sshKeyMetadata, proxyUsername, proxyPassword, initScriptPath)

	zlog.InfoWithCtx(c, "Executing gcloud command to create VM", "command", cmdStr)

	stdout, stderr, _, err := tool.ExecCommand(cmdStr)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to create VM", err)
		return nil, fmt.Errorf("failed to create VM: %v, stderr: %s", err, stderr)
	}

	zlog.InfoWithCtx(c, "VM creation command completed", "stdout", stdout, "stderr", stderr)

	// 等待VM启动并重试获取外网IP
	externalIP := "pending"
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		waitTime := i * 10 //
		zlog.InfoWithCtx(c, "Waiting for VM to get external IP", "attempt", i+1, "waitTime", waitTime)
		time.Sleep(time.Duration(waitTime) * time.Second)

		ip, err := s.getVMExternalIP(c, vmName, zone)
		if err == nil {
			externalIP = ip
			zlog.InfoWithCtx(c, "Successfully got external IP", "ip", ip, "attempt", i+1)
			break
		}
		zlog.InfoWithCtx(c, "Failed to get external IP, will retry", "error", err, "attempt", i+1)
	}

	if externalIP == "pending" {
		zlog.InfoWithCtx(c, "Failed to get external IP after all retries, VM created but IP is pending")
	}

	// 根据代理类型构建代理地址
	var proxyAuth string
	if proxyType == constants.ProxyTypeTinyProxy {
		// TinyProxy HTTP代理使用8080端口，不使用认证
		proxyAuth = fmt.Sprintf("http://%s:8080", externalIP)
	} else if proxyType == constants.ProxyTypeHttpProxy {
		// 自定义HTTP代理使用1081端口，路径代理模式
		proxyAuth = fmt.Sprintf("http://%s:1081/px", externalIP)
	} else {
		// SOCKS5代理使用1080端口
		proxyAuth = fmt.Sprintf("%s:%s@%s:1080", proxyUsername, proxyPassword, externalIP)
	}

	vmInstance := &dao.VMInstance{
		VMID:        vmName,
		VMName:      vmName,
		Zone:        zone,
		MachineType: machineType,
		ExternalIP:  externalIP,
		Proxy:       proxyAuth,
		ProxyType:   proxyType,
		SSHUser:     "gatc",
		// SSHKeyContent: gcpConfig.GetSSHKeyContent(),
		Status: constants.VMStatusRunning,
	}

	if err := dao.GVmInstanceDao.Create(c, vmInstance); err != nil {
		zlog.ErrorWithCtx(c, "Failed to save VM to database, but VM created successfully", err)
	}

	return &CreateVMResult{
		VMID:          vmName,
		VMName:        vmName,
		ExternalIP:    externalIP,
		Proxy:         proxyAuth,
		SSHConnection: fmt.Sprintf("ssh -i ./conf/gcp/gatc_rsa gatc@%s", externalIP),
	}, nil
}

func (s *VMService) getVMExternalIP(c *gin.Context, vmName, zone string) (string, error) {
	gcpConfig := config.GetGCPConfig()
	projectID := gcpConfig.GetProjectID()

	cmdStr := fmt.Sprintf("gcloud compute instances describe %s "+
		"--project=%s --zone=%s --format='value(networkInterfaces[0].accessConfigs[0].natIP)'",
		vmName, projectID, zone)

	zlog.InfoWithCtx(c, "Getting VM external IP", "command", cmdStr)

	stdout, stderr, _, err := tool.ExecCommand(cmdStr)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to get external IP", err)
		return "", fmt.Errorf("failed to get external IP: %v, stderr: %s", err, stderr)
	}

	ip := strings.TrimSpace(stdout)
	if ip == "" {
		return "", fmt.Errorf("external IP not found")
	}

	zlog.InfoWithCtx(c, "Got VM external IP", "ip", ip)
	return ip, nil
}

func (s *VMService) DeleteVM(c *gin.Context, param *DeleteVMParam) (*DeleteVMResult, error) {
	vmInstance, err := dao.GVmInstanceDao.GetByVMID(c, param.VMID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return &DeleteVMResult{
				Success: false,
				Message: "VM not found",
				VMID:    param.VMID,
			}, fmt.Errorf("VM not found")
		}
		return &DeleteVMResult{
			Success: false,
			Message: "Failed to query VM",
			VMID:    param.VMID,
		}, fmt.Errorf("failed to query VM: %v", err)
	}

	gcpConfig := config.GetGCPConfig()
	projectID := gcpConfig.GetProjectID()

	cmdStr := fmt.Sprintf("gcloud compute instances delete %s "+
		"--project=%s --zone=%s --quiet", param.VMID, projectID, vmInstance.Zone)

	zlog.InfoWithCtx(c, "Deleting VM from GCP", "command", cmdStr)

	stdout, _, _, err := tool.ExecCommand(cmdStr)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to delete VM from GCP", err)
	} else {
		zlog.InfoWithCtx(c, "VM deleted from GCP successfully", "stdout", stdout)
	}

	if err := dao.GVmInstanceDao.UpdateStatus(c, param.VMID, constants.VMStatusDeleted); err != nil {
		return &DeleteVMResult{
			Success: false,
			Message: "Failed to update VM status",
			VMID:    param.VMID,
		}, fmt.Errorf("failed to update VM status: %v", err)
	}

	return &DeleteVMResult{
		Success: true,
		Message: "VM deleted successfully",
		VMID:    param.VMID,
	}, nil
}

func (s *VMService) ListVMs(c *gin.Context, param *ListVMParam) (*ListVMResult, error) {
	page := 1
	if param.Page > 0 {
		page = param.Page
	}

	size := 10
	if param.Size > 0 && param.Size <= 100 {
		size = param.Size
	}

	offset := (page - 1) * size

	items, total, err := dao.GVmInstanceDao.List(c, param.Status, offset, size)
	if err != nil {
		return nil, fmt.Errorf("failed to query VMs: %v", err)
	}

	return &ListVMResult{
		Total: total,
		Page:  page,
		Size:  size,
		Items: items,
	}, nil
}

func (s *VMService) GetVM(c *gin.Context, param *GetVMParam) (*GetVMResult, error) {
	vmInstance, err := dao.GVmInstanceDao.GetByVMID(c, param.VMID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("VM not found")
		}
		return nil, fmt.Errorf("failed to query VM: %v", err)
	}

	return &GetVMResult{
		VMInstance: vmInstance,
	}, nil
}

func (s *VMService) RefreshVMIP(c *gin.Context, param *RefreshVMIPParam) (*RefreshVMIPResult, error) {
	// 先查询VM是否存在
	vmInstance, err := dao.GVmInstanceDao.GetByVMID(c, param.VMID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("VM not found")
		}
		return nil, fmt.Errorf("failed to query VM: %v", err)
	}

	// 获取最新的外网IP
	newIP, err := s.getVMExternalIP(c, vmInstance.VMID, vmInstance.Zone)
	if err != nil {
		return &RefreshVMIPResult{
			VMID:       param.VMID,
			ExternalIP: vmInstance.ExternalIP, // 返回原来的IP
			Updated:    false,
		}, fmt.Errorf("failed to get external IP: %v", err)
	}

	// 如果IP有变化，更新数据库
	updated := false
	if newIP != vmInstance.ExternalIP {
		vmInstance.ExternalIP = newIP
		if err := dao.GVmInstanceDao.Save(c, vmInstance); err != nil {
			return nil, fmt.Errorf("failed to update VM external IP: %v", err)
		}
		updated = true
		zlog.InfoWithCtx(c, "VM external IP updated", "vmId", param.VMID, "newIP", newIP, "oldIP", vmInstance.ExternalIP)
	}

	return &RefreshVMIPResult{
		VMID:       param.VMID,
		ExternalIP: newIP,
		Updated:    updated,
	}, nil
}

// GCPVMInstance GCP中的VM实例信息
type GCPVMInstance struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Zone        string `json:"zone"`
	MachineType string `json:"machineType"`
	Status      string `json:"status"`
	ExternalIP  string `json:"externalIP"`
	InternalIP  string `json:"internalIP"`
}

// getGCPVMInstances 获取GCP中所有VM实例
func (s *VMService) getGCPVMInstances(c *gin.Context) ([]GCPVMInstance, error) {
	if err := s.activateServiceAccount(c); err != nil {
		return nil, fmt.Errorf("failed to activate service account: %v", err)
	}

	gcpConfig := config.GetGCPConfig()
	projectID := gcpConfig.GetProjectID()

	cmdStr := fmt.Sprintf("gcloud compute instances list --project=%s --format=json", projectID)

	zlog.InfoWithCtx(c, "Getting GCP VM instances", "command", cmdStr)

	stdout, stderr, _, err := tool.ExecCommand(cmdStr)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to get GCP VM instances", err)
		return nil, fmt.Errorf("failed to get GCP VM instances: %v, stderr: %s", err, stderr)
	}

	var rawInstances []map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &rawInstances); err != nil {
		zlog.ErrorWithCtx(c, "Failed to parse GCP VM instances JSON", err)
		return nil, fmt.Errorf("failed to parse GCP VM instances JSON: %v", err)
	}

	var instances []GCPVMInstance
	for _, raw := range rawInstances {
		instance := GCPVMInstance{}

		if id, ok := raw["id"].(string); ok {
			instance.ID = id
		}

		if name, ok := raw["name"].(string); ok {
			instance.Name = name
		}

		if zone, ok := raw["zone"].(string); ok {
			parts := strings.Split(zone, "/")
			if len(parts) > 0 {
				instance.Zone = parts[len(parts)-1]
			}
		}

		if machineType, ok := raw["machineType"].(string); ok {
			parts := strings.Split(machineType, "/")
			if len(parts) > 0 {
				instance.MachineType = parts[len(parts)-1]
			}
		}

		if status, ok := raw["status"].(string); ok {
			instance.Status = status
		}

		// 获取网络接口信息
		if networkInterfaces, ok := raw["networkInterfaces"].([]interface{}); ok && len(networkInterfaces) > 0 {
			if firstInterface, ok := networkInterfaces[0].(map[string]interface{}); ok {
				if networkIP, ok := firstInterface["networkIP"].(string); ok {
					instance.InternalIP = networkIP
				}

				if accessConfigs, ok := firstInterface["accessConfigs"].([]interface{}); ok && len(accessConfigs) > 0 {
					if firstAccess, ok := accessConfigs[0].(map[string]interface{}); ok {
						if natIP, ok := firstAccess["natIP"].(string); ok {
							instance.ExternalIP = natIP
						}
					}
				}
			}
		}

		instances = append(instances, instance)
	}

	zlog.InfoWithCtx(c, "Found GCP VM instances", "count", len(instances))
	return instances, nil
}

// SyncVMsWithGCP VM信息同步到数据库的定时任务
func (s *VMService) BatchCreateVM(c *gin.Context, param *BatchCreateVMParam) (*BatchCreateVMResult, error) {
	if param.Num <= 0 {
		return nil, fmt.Errorf("num must be greater than 0")
	}
	if param.Num > 100 {
		return nil, fmt.Errorf("num cannot exceed 100")
	}

	proxyType := constants.ProxyTypeSocks5
	if param.ProxyType != "" {
		if param.ProxyType == constants.ProxyTypeTinyProxy {
			proxyType = constants.ProxyTypeTinyProxy
		} else if param.ProxyType == constants.ProxyTypeHttpProxy || param.ProxyType == constants.ProxyTypeHttpProxyAlias {
			proxyType = constants.ProxyTypeHttpProxy
		}
	}

	prefix := fmt.Sprintf("gatcvm-%s-%s-", strings.ToLower(proxyType), strings.ToLower(param.Tag))

	existingVMs, err := dao.GVmInstanceDao.GetByPrefix(c, prefix, 1)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing VMs: %v", err)
	}
	if len(existingVMs) > 0 {
		return nil, fmt.Errorf("请勿重复创建或更改tag重试")
	}

	result := &BatchCreateVMResult{
		Total:   param.Num,
		Results: make([]CreateVMResult, param.Num),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < param.Num; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()

			createParam := &CreateVMParam{
				Zone:        param.Zone,
				MachineType: param.MachineType,
				Tag:         param.Tag + "-" + strconv.Itoa(i),
				ProxyType:   param.ProxyType,
			}

			vmResult, err := s.CreateVM(c, createParam)

			mu.Lock()
			if err != nil {
				zlog.ErrorWithCtx(c, "Batch create VM failed", err)
				result.Failed++
				result.Results[index] = CreateVMResult{}
			} else {
				result.Success++
				result.Results[index] = *vmResult
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	zlog.InfoWithCtx(c, "Batch create VMs completed", "total", result.Total, "success", result.Success, "failed", result.Failed)
	return result, nil
}

func (s *VMService) BatchDeleteVM(c *gin.Context, param *BatchDeleteVMParam) (*BatchDeleteVMResult, error) {
	var vmIDs []string

	if len(param.VMList) > 0 {
		vmIDs = param.VMList
	} else if param.Prefix != "" {
		limit := param.Limit
		if limit <= 0 {
			limit = 1000
		}

		vms, err := dao.GVmInstanceDao.GetByPrefix(c, param.Prefix, limit)
		if err != nil {
			return nil, fmt.Errorf("failed to query VMs by prefix: %v", err)
		}

		for _, vm := range vms {
			vmIDs = append(vmIDs, vm.VMID)
		}
	} else {
		return nil, fmt.Errorf("either vm_list or prefix must be provided")
	}

	if len(vmIDs) == 0 {
		return &BatchDeleteVMResult{Total: 0, Success: 0, Failed: 0, Results: []DeleteVMResult{}}, nil
	}

	result := &BatchDeleteVMResult{
		Total:   len(vmIDs),
		Results: make([]DeleteVMResult, len(vmIDs)),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, vmID := range vmIDs {
		wg.Add(1)
		go func(index int, id string) {
			defer wg.Done()

			deleteParam := &DeleteVMParam{VMID: id}
			deleteResult, err := s.DeleteVM(c, deleteParam)

			mu.Lock()
			if err != nil {
				zlog.ErrorWithCtx(c, "Batch delete VM failed", err)
				result.Failed++
				result.Results[index] = DeleteVMResult{Success: false, Message: err.Error(), VMID: vmID}
			} else {
				result.Success++
				result.Results[index] = *deleteResult
			}
			mu.Unlock()
		}(i, vmID)
	}

	wg.Wait()

	zlog.InfoWithCtx(c, "Batch delete VMs completed", "total", result.Total, "success", result.Success, "failed", result.Failed)
	return result, nil
}

func (s *VMService) SyncVMsWithGCP() {
	c := &gin.Context{}

	zlog.InfoWithCtx(c, "SyncVMsWithGCP Starting VM sync with GCP")

	// 获取GCP中所有VM实例 (集合A)
	gcpInstances, err := s.getGCPVMInstances(c)
	if err != nil {
		zlog.ErrorWithCtx(c, "SyncVMsWithGCP Failed to get GCP VM instances during sync", err)
		return
	}

	// 获取数据库中非已删除的记录 (集合B)
	dbInstances, err := dao.GVmInstanceDao.GetActiveVMs(c)
	if err != nil {
		zlog.ErrorWithCtx(c, "SyncVMsWithGCP Failed to get active VM instances from DB during sync", err)
		return
	}

	// 创建GCP实例名称的集合 (只包含GATC创建的VM)
	gcpVMIds := make(map[string]GCPVMInstance)
	for _, gcpVM := range gcpInstances {
		if s.isGATCVM(gcpVM.ID) {
			gcpVMIds[gcpVM.ID] = gcpVM
		}
	}

	// 创建数据库实例名称的集合 (只包含GATC创建的VM)
	dbVMIds := make(map[string]dao.VMInstance)
	for _, dbVM := range dbInstances {
		if s.isGATCVM(dbVM.VMID) {
			dbVMIds[dbVM.VMID] = dbVM
		}
	}

	var toDeleteVMIDs []string
	/*  移除删除sync 删db的逻辑， 获取getGCPVMInstances 可能异常
	// B-A: 数据库中有但GCP中没有的GATC VM，设置为删除状态
	for ID := range dbVMIds {
		if _, exists := gcpVMIds[ID]; !exists {
			toDeleteVMIDs = append(toDeleteVMIDs, ID)
		}
	}

	if len(toDeleteVMIDs) > 0 {
		if err := dao.GVmInstanceDao.BatchUpdateStatusDeleted(c, toDeleteVMIDs, constants.VMStatusDeleted); err != nil {
			zlog.ErrorWithCtx(c, "SyncVMsWithGCP Failed to batch delete VMs during sync", err)
		} else {
			zlog.InfoWithCtx(c, "SyncVMsWithGCP Marked VMs as deleted during sync", "count", len(toDeleteVMIDs), "vmIds", toDeleteVMIDs)
		}
	}
	*/

	// A-B: GCP中有但数据库中没有的GATC VM，插入到数据库
	var toInsertVMs []*dao.VMInstance
	for vmId, gcpVM := range gcpVMIds {
		if _, exists := dbVMIds[vmId]; !exists {
			// 只插入运行状态的GATC VM
			if gcpVM.Status == "RUNNING" {
				newVM := &dao.VMInstance{
					VMID:        gcpVM.ID,
					VMName:      gcpVM.Name,
					Zone:        gcpVM.Zone,
					MachineType: gcpVM.MachineType,
					ExternalIP:  gcpVM.ExternalIP,
					InternalIP:  gcpVM.InternalIP,
					Status:      constants.VMStatusRunning,
					SSHUser:     "gatc", // 默认SSH用户
				}
				toInsertVMs = append(toInsertVMs, newVM)
			}
		}
	}

	// 批量插入新VM
	successCount := 0
	for _, vm := range toInsertVMs {
		if err := dao.GVmInstanceDao.Create(c, vm); err != nil {
			zlog.ErrorWithCtx(c, "SyncVMsWithGCP Failed to insert VM during sync", err)
		} else {
			successCount++
		}
	}

	if len(toInsertVMs) > 0 {
		zlog.InfoWithCtx(c, "SyncVMsWithGCP Inserted new GATC VMs during sync", "attempted", len(toInsertVMs), "success", successCount)
	}

	zlog.InfoWithCtx(c, "SyncVMsWithGCP GATC VM sync with GCP completed",
		"totalGCPVMs", len(gcpInstances),
		"gatcGCPVMs", len(gcpVMIds),
		"totalDBVMs", len(dbInstances),
		"gatcDBVMs", len(dbVMIds),
		"deleted", len(toDeleteVMIDs),
		"inserted", successCount)
}

// ReplaceProxyResourceParam 替换代理资源参数
type ReplaceProxyResourceParam struct {
	BatchCreateVMParam
}

// ReplaceProxyResourceResult 替换代理资源结果
type ReplaceProxyResourceResult struct {
	NewVMsCreated      int                  `json:"new_vms_created"`
	CreateVms          *BatchCreateVMResult `json:"create_vms"`
	NewProxiesAdded    int                  `json:"new_proxies_added"`
	OldProxiesDisabled int                  `json:"old_proxies_disabled"`
	TokensUpdated      int64                `json:"tokens_updated"`
	AsyncDeletedVMIDs  []string             `json:"async_deleted_vm_ids"`
	Message            string               `json:"message"`
	Warn               string               `json:"warn"`
}

// ReplaceProxyResource 替换代理资源
func (s *VMService) ReplaceProxyResource(c *gin.Context, param *ReplaceProxyResourceParam) (*ReplaceProxyResourceResult, error) {
	// 验证proxy_type必须是server或httpProxyServer
	if param.ProxyType != constants.ProxyTypeHttpProxyAlias && param.ProxyType != constants.ProxyTypeHttpProxy {
		return nil, fmt.Errorf("proxy_type必须是'server'或'httpProxyServer'，当前值: %s", param.ProxyType)
	}

	result := &ReplaceProxyResourceResult{}

	// 步骤1: 创建新的VM（代理机）
	zlog.InfoWithCtx(c, "ReplaceProxyResource Step 1: Creating new VMs", "num", param.Num)
	batchCreateResult, err := s.BatchCreateVM(c, &param.BatchCreateVMParam)
	if err != nil {
		return nil, fmt.Errorf("创建新VM失败: %v", err)
	}
	if batchCreateResult.Success == 0 {
		return nil, fmt.Errorf("所有VM创建失败")
	}
	result.NewVMsCreated = batchCreateResult.Success
	result.CreateVms = batchCreateResult
	zlog.InfoWithCtx(c, "ReplaceProxyResource VMs created", "success", batchCreateResult.Success, "failed", batchCreateResult.Failed)

	// 步骤2: 查询proxy_pool表，按created_at倒序limit num个（状态为active的）
	zlog.InfoWithCtx(c, "ReplaceProxyResource Step 2: Querying last batch proxies from proxy_pool")
	lastBatchProxy, err := dao.GProxyPoolDao.GetLastBatchByType(c, constants.ProxyTypeHttpProxyAlias, dao.ProxyStatusActive, param.Num)
	if err != nil {
		return nil, fmt.Errorf("查询旧代理失败: %v", err)
	}
	zlog.InfoWithCtx(c, "ReplaceProxyResource Found old proxies", "count", len(lastBatchProxy))

	// 步骤6: 将lastBatchProxy的status置为0  (步骤6提前， proxy_pool 有多个代理通Ip的情况，gcloud vm有Ip回收)
	zlog.InfoWithCtx(c, "ReplaceProxyResource Step 6: Disabling old proxies")
	var oldProxyIDs []int64
	for _, proxy := range lastBatchProxy {
		oldProxyIDs = append(oldProxyIDs, proxy.ID)
	}
	if len(oldProxyIDs) > 0 {
		if err := dao.GProxyPoolDao.BatchUpdateStatus(c, oldProxyIDs, dao.ProxyStatusDeleted); err != nil {
			zlog.ErrorWithCtx(c, "Failed to disable old proxies", err)
		} else {
			result.OldProxiesDisabled = len(oldProxyIDs)
			zlog.InfoWithCtx(c, "ReplaceProxyResource Old proxies disabled", "count", len(oldProxyIDs))
		}
	}

	// 步骤3: 将新VM的代理插入proxy_pool表
	zlog.InfoWithCtx(c, "ReplaceProxyResource Step 3: Inserting new proxies to proxy_pool")
	var newProxies []dao.ProxyPool
	for _, vmResult := range batchCreateResult.Results {
		// proxy格式: "http://35.208.147.190:1081" (不含/px后缀)
		proxyURL := strings.TrimRight(vmResult.Proxy, "/px")
		if proxyURL == "" {
			result.Warn += fmt.Sprintf("\tvm %s proxy illegal,skip", vmResult.VMID)
			continue
		}
		newProxies = append(newProxies, dao.ProxyPool{
			Proxy:     proxyURL,
			ProxyType: constants.ProxyTypeHttpProxyAlias,
			Status:    dao.ProxyStatusActive,
			FromVM:    1, // 标记为来自VM
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
	}

	if len(newProxies) > 0 {
		if err := dao.GProxyPoolDao.BatchCreate(c, newProxies); err != nil {
			return nil, fmt.Errorf("插入新代理到proxy_pool失败: %v", err)
		}
		result.NewProxiesAdded = len(newProxies)
		zlog.InfoWithCtx(c, "ReplaceProxyResource New proxies inserted", "count", len(newProxies))
	}

	// 步骤4: 建立新旧代理1:1映射
	zlog.InfoWithCtx(c, "ReplaceProxyResource Step 4: Building old-new proxy mapping")
	replaceCount := len(lastBatchProxy)
	if replaceCount > len(newProxies) {
		replaceCount = len(newProxies)
	}

	toReplaceMap := make(map[string]string) // oldProxy -> newProxy
	for i := 0; i < replaceCount; i++ {
		toReplaceMap[lastBatchProxy[i].Proxy] = newProxies[i].Proxy
	}
	zlog.InfoWithCtx(c, "ReplaceProxyResource Mapping created", "pairs", len(toReplaceMap))

	// 步骤5: 替换official_tokens表中的base_url
	zlog.InfoWithCtx(c, "ReplaceProxyResource Step 5: Replacing base_url in official_tokens")
	tokenDao := &dao.GormOfficialTokens{}
	var totalTokensUpdated int64
	for oldProxy, newProxy := range toReplaceMap {
		affectedRows, err := tokenDao.ReplaceBaseURLProxy(c, oldProxy, newProxy)
		if err != nil {
			zlog.ErrorWithCtx(c, fmt.Sprintf("Failed to replace proxy in tokens, oldProxy=%s, newProxy=%s", oldProxy, newProxy), err)
			continue
		}
		totalTokensUpdated += affectedRows
		zlog.InfoWithCtx(c, "ReplaceProxyResource Replaced proxy in tokens", "oldProxy", oldProxy, "newProxy", newProxy, "affected", affectedRows)
	}
	result.TokensUpdated = totalTokensUpdated
	zlog.InfoWithCtx(c, "ReplaceProxyResource Total tokens updated", "count", totalTokensUpdated)

	// 步骤7: 获取旧代理对应的VM ID
	zlog.InfoWithCtx(c, "ReplaceProxyResource Step 7: Finding old VMs to delete")
	var toDelVMIDs []string
	for _, oldProxy := range lastBatchProxy {
		// 旧代理加/px后缀去vm_instance表查询
		proxyWithSuffix := oldProxy.Proxy + "/px"
		vmInstance, err := dao.GVmInstanceDao.GetByProxy(c, proxyWithSuffix)
		if err != nil {
			zlog.ErrorWithCtx(c, fmt.Sprintf("Failed to find VM by proxy: %s", proxyWithSuffix), err)
			continue
		}
		if vmInstance != nil {
			toDelVMIDs = append(toDelVMIDs, vmInstance.VMID)
		}
	}
	zlog.InfoWithCtx(c, "ReplaceProxyResource VMs to delete", "count", len(toDelVMIDs))

	// 步骤8: 删除旧VM
	if len(toDelVMIDs) > 0 {
		zlog.InfoWithCtx(c, "ReplaceProxyResource Step 8: Deleting old VMs")
		batchDeleteParam := &BatchDeleteVMParam{
			VMList: toDelVMIDs,
		}
		result.AsyncDeletedVMIDs = toDelVMIDs
		go func() {
			time.Sleep(70 * time.Second)
			deleteResult, err := s.BatchDeleteVM(c, batchDeleteParam)
			if err != nil {
				zlog.ErrorWithCtx(c, "Failed to delete old VMs", err)
			} else {
				zlog.InfoWithCtx(c, "ReplaceProxyResource Old VMs deleted", "success", deleteResult.Success, "failed", deleteResult.Failed)
			}
		}()

	}
	result.Message = fmt.Sprintf("代理资源替换完成: 新建VM %d个, 新增代理 %d个, 禁用旧代理 %d个, 更新Token %d个, 触发删除旧VM %d个",
		result.NewVMsCreated, result.NewProxiesAdded, result.OldProxiesDisabled, result.TokensUpdated, len(result.AsyncDeletedVMIDs))

	zlog.InfoWithCtx(c, "ReplaceProxyResource Completed", "result", result)
	return result, nil
}

// ReplaceProxyResourceV2Result V2版本替换代理资源结果
type ReplaceProxyResourceV2Result struct {
	MarkedPendingDelete int                  `json:"marked_pending_delete"` // 标记为预删除的VM数量
	NewVMsCreated       int                  `json:"new_vms_created"`
	CreateVms           *BatchCreateVMResult `json:"create_vms"`
	Message             string               `json:"message"`
}

// ReplaceProxyResourceV2 替换代理资源V2版本
// 逻辑：先标记旧VM为预删除状态，然后创建新VM，由定时任务负责真正删除
func (s *VMService) ReplaceProxyResourceV2(c *gin.Context, param *ReplaceProxyResourceParam) (result ReplaceProxyResourceV2Result, err error) {
	// 验证proxy_type必须是server或httpProxyServer
	if param.ProxyType != constants.ProxyTypeHttpProxyAlias && param.ProxyType != constants.ProxyTypeHttpProxy {
		err = fmt.Errorf("proxy_type必须是'server'或'httpProxyServer'，当前值: %s", param.ProxyType)
		return
	}

	// 步骤1: 按Zone、MachineType、ProxyType查询最多num个Running状态的VM
	zlog.InfoWithCtx(c, "ReplaceProxyResourceV2 Step 1: Querying VMs to mark as pending delete",
		"zone", param.Zone, "machineType", param.MachineType, "proxyType", param.ProxyType, "num", param.Num)

	zone := param.Zone
	if zone == "" {
		zone = constants.DefaultZone
	}
	machineType := param.MachineType
	if machineType == "" {
		machineType = constants.DefaultMachineType
	}

	oldVMs, err := dao.GVmInstanceDao.GetByConditions(c, zone, machineType, param.ProxyType, constants.VMStatusRunning, param.Num)
	if err != nil {
		err = fmt.Errorf("查询旧VM失败: %v", err)
		return
	}
	zlog.InfoWithCtx(c, "ReplaceProxyResourceV2 Found VMs to replace", "count", len(oldVMs))

	// 步骤2: 将这些VM的状态设置为预删除状态
	if len(oldVMs) > 0 {
		var vmIDs []string
		for _, vm := range oldVMs {
			vmIDs = append(vmIDs, vm.VMID)
		}

		if err := dao.GVmInstanceDao.BatchUpdateStatusByIDs(c, vmIDs, constants.VMStatusPendingDelete); err != nil {
			err = fmt.Errorf("标记VM为预删除状态失败: %v", err)
			return result, err
		}
		result.MarkedPendingDelete = len(vmIDs)
		zlog.InfoWithCtx(c, "ReplaceProxyResourceV2 Marked VMs as pending delete", "count", len(vmIDs))
	}

	// 步骤3: 创建num个新代理VM
	zlog.InfoWithCtx(c, "ReplaceProxyResourceV2 Step 2: Creating new VMs", "num", param.Num)
	batchCreateResult, err := s.BatchCreateVM(c, &param.BatchCreateVMParam)
	if err != nil {
		err = fmt.Errorf("创建新VM失败: %v", err)
		return
	}
	if batchCreateResult.Success == 0 {
		err = fmt.Errorf("所有VM创建失败")
		return
	}
	result.NewVMsCreated = batchCreateResult.Success
	result.CreateVms = batchCreateResult
	zlog.InfoWithCtx(c, "ReplaceProxyResourceV2 VMs created", "success", batchCreateResult.Success, "failed", batchCreateResult.Failed)

	zlog.InfoWithCtx(c, "ReplaceProxyResourceV2 Completed", "result", result)
	return
}

// CleanupPendingDeleteVMs 清理预删除状态的VM（定时任务）
func (s *VMService) CleanupPendingDeleteVMs() {
	c := &gin.Context{}

	zlog.InfoWithCtx(c, "CleanupPendingDeleteVMs Starting cleanup of pending delete VMs")

	// 获取更新时间在指定小时之前的预删除状态VM
	cutoffTime := time.Now().Add(-time.Duration(constants.VMPendingDeleteRetentionHours) * time.Hour)

	vms, err := dao.GVmInstanceDao.GetPendingDeleteVMsBeforeTime(c, cutoffTime)
	if err != nil {
		zlog.ErrorWithCtx(c, "CleanupPendingDeleteVMs Failed to get pending delete VMs", err)
		return
	}

	if len(vms) == 0 {
		zlog.InfoWithCtx(c, "CleanupPendingDeleteVMs No pending delete VMs to cleanup")
		return
	}

	zlog.InfoWithCtx(c, "CleanupPendingDeleteVMs Found pending delete VMs", "count", len(vms))

	// 收集VM ID列表
	var vmIDs []string
	for _, vm := range vms {
		vmIDs = append(vmIDs, vm.VMID)
	}

	// 调用批量删除服务
	batchDeleteParam := &BatchDeleteVMParam{
		VMList: vmIDs,
	}

	deleteResult, err := s.BatchDeleteVM(c, batchDeleteParam)
	if err != nil {
		zlog.ErrorWithCtx(c, "CleanupPendingDeleteVMs Failed to batch delete VMs", err)
		return
	}

	zlog.InfoWithCtx(c, "CleanupPendingDeleteVMs Cleanup completed",
		"total", deleteResult.Total,
		"success", deleteResult.Success,
		"failed", deleteResult.Failed)
}

type SyncProxyPoolFromVMsRes struct {
	OldFromVmProxyCount int      `json:"old_from_vm_proxy_count"`
	ActiveVmCount       int      `json:"active_vm_count"`
	DelProxiesCount     int      `json:"del_proxies_count"`
	DelProxies          []int64  `json:"del_proxies"`
	AddProxyCount       int      `json:"add_proxy_count"`
	AddProxies          []string `json:"add_proxies"`
	ErrMsg              string   `json:"err_msg"`
}

// SyncProxyPoolFromVMs 从VM同步代理池
// 逻辑：
// 1. 查询 proxy_pool 表中 from_vm > 0 的记录作为 set1
// 2. 查询 vm_instances 表中 status = Running 的 VM 作为 set2
// 3. 遍历 set1，跳过非server类型，不在set2中的设置为deleted，在set2中的标记VM为已处理
// 4. 遍历 set2，对未处理的插入新的 ProxyPool 记录
func (s *VMService) SyncProxyPoolFromVMs(c *gin.Context) (res SyncProxyPoolFromVMsRes,err error) {
	zlog.InfoWithCtx(c, "SyncProxyPoolFromVMs Starting sync proxy pool from VMs")

	// 步骤1: 查询 proxy_pool 表中 from_vm > 0 的记录
	set1, err := dao.GProxyPoolDao.GetFromVMProxies(c)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to get proxy pool records from VM", err)
		err = fmt.Errorf("查询proxy_pool失败: %v", err)
		res.ErrMsg = err.Error()
		return
	}
	res.OldFromVmProxyCount = len(set1)
	zlog.InfoWithCtx(c, "SyncProxyPoolFromVMs Found proxy pool records from VM", "count", len(set1))

	// 步骤2: 查询 vm_instances 表中 status = Running 的 VM
	set2, err := dao.GVmInstanceDao.GetActiveVMs(c)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to get active VMs", err)
		err = fmt.Errorf("查询active VMs失败: %v", err)
		return
	}
	res.ActiveVmCount = len(set2)
	zlog.InfoWithCtx(c, "SyncProxyPoolFromVMs Found active VMs", "count", len(set2))

	// 构建 set2 的 proxy 映射（去掉 /px 后缀）
	// key: proxy (不含/px), value: VM对象的指针（用于标记处理状态）
	type VMWithFlag struct {
		VM        dao.VMInstance
		Processed bool
	}
	set2Map := make(map[string]*VMWithFlag)
	for _, vm := range set2 {
		// vm_instances.proxy 格式: "http://IP:1081/px"
		// proxy_pool.proxy 格式: "http://IP:1081"
		proxyWithoutSuffix := strings.TrimSuffix(vm.Proxy, "/px")
		if proxyWithoutSuffix != "" && vm.ProxyType == constants.ProxyTypeHttpProxyAlias {
			set2Map[proxyWithoutSuffix] = &VMWithFlag{
				VM:        vm,
				Processed: false,
			}
		}
	}
	zlog.InfoWithCtx(c, "SyncProxyPoolFromVMs Built VM proxy map", "count", len(set2Map))

	// 步骤3: 遍历 set1
	var toDeleteProxyIDs []int64
	for _, proxyRecord := range set1 {
		// 跳过非 server 类型
		if proxyRecord.ProxyType != constants.ProxyTypeHttpProxyAlias {
			continue
		}

		// 检查是否在 set2 中
		if vmWithFlag, exists := set2Map[proxyRecord.Proxy]; exists {
			vmWithFlag.Processed = true
		} else {
			toDeleteProxyIDs = append(toDeleteProxyIDs, proxyRecord.ID)
		}
	}
	res.DelProxiesCount = len(toDeleteProxyIDs)
	res.DelProxies = toDeleteProxyIDs

	// 批量更新待删除的代理状态
	if len(toDeleteProxyIDs) > 0 {
		if err2 := dao.GProxyPoolDao.BatchUpdateStatus(c, toDeleteProxyIDs, dao.ProxyStatusDeleted); err2 != nil {
			zlog.ErrorWithCtx(c, "Failed to update deleted proxies status", err2)
			err = fmt.Errorf("Failed to update deleted proxies status, %w", err2)
			res.ErrMsg = err.Error()
			return
		} else {
			zlog.InfoWithCtx(c, "SyncProxyPoolFromVMs Updated deleted proxies status", "count", len(toDeleteProxyIDs))
		}
	}

	// 步骤4: 遍历 set2，对未处理的插入新记录
	var newProxies []dao.ProxyPool
	for proxy, vmWithFlag := range set2Map {
		if !vmWithFlag.Processed {
			// 未处理的，需要插入新记录
			newProxies = append(newProxies, dao.ProxyPool{
				Proxy:     proxy,
				ProxyType: constants.ProxyTypeHttpProxyAlias,
				Status:    dao.ProxyStatusActive,
				FromVM:    1, // 标记为来自VM
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			})
			res.AddProxies = append(res.AddProxies, proxy)
		}
	}

	// 批量插入新代理
	if len(newProxies) > 0 {
		if err2 := dao.GProxyPoolDao.BatchCreate(c, newProxies); err2 != nil {
			zlog.ErrorWithCtx(c, "Failed to insert new proxies", err2)
			err = fmt.Errorf("插入新代理失败: %v", err2)
			res.ErrMsg = err.Error()
			return
		}
		res.AddProxyCount = len(newProxies)
		zlog.InfoWithCtx(c, "SyncProxyPoolFromVMs Inserted new proxies", "count", len(newProxies))
	}

	zlog.InfoWithCtx(c, "SyncProxyPoolFromVMs Sync completed",
		"deleted", len(toDeleteProxyIDs),
		"inserted", len(newProxies))

	return 
}
