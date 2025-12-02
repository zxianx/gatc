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
}

// DeleteVMParam 删除VM输入参数
type DeleteVMParam struct {
	VMID string `json:"vm_id"`
}

// DeleteVMResult 删除VM返回结果
type DeleteVMResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
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
		zlog.Info("VM cleanup already running, skipping this execution")
		return
	}
	defer cleanupRunning.Store(false)

	c := &gin.Context{}

	// 每小时清理24小时前的VM
	existH := os.Getenv("CLEAN_OLD_VM_EXIST_EXCEED_H")
	if existH == "" {
		zlog.InfoWithCtx(c, "SKIP. Starting cleanup of old VMs")
		return
	}
	h, err2 := strconv.Atoi(existH)
	if err2 != nil {
		zlog.Error("CleanupOldVMs ", "illegal CLEAN_OLD_VM_EXIST_EXCEED_H, val:", existH)
		return
	}

	zlog.InfoWithCtx(c, "Starting cleanup of old VMs, ", h)

	// 获取24小时前创建的VM
	cutoffTime := time.Now().Add(-time.Duration(h) * time.Hour)

	vms, err := dao.GVmInstanceDao.GetVMsCreatedBefore(c, cutoffTime)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to get old VMs", err)
		return
	}

	if len(vms) == 0 {
		zlog.InfoWithCtx(c, "No old VMs to cleanup")
		return
	}

	successCount := 0
	for _, vm := range vms {
		// 只处理GATC创建的VM
		if !s.isGATCVM(vm.VMID) {
			zlog.InfoWithCtx(c, "Skipping non-GATC VM during cleanup", "vmId", vm.VMID)
			continue
		}

		// 删除GCP中的VM实例
		if err := s.deleteVMFromGCP(c, &vm); err != nil {
			zlog.ErrorWithCtx(c, "Failed to delete VM from GCP", err)
			continue
		}

		// 更新数据库状态
		if err := dao.GVmInstanceDao.UpdateStatus(c, vm.VMID, constants.VMStatusDeleted); err != nil {
			zlog.ErrorWithCtx(c, "Failed to update VM status", err)
			continue
		}

		successCount++
		zlog.InfoWithCtx(c, "Deleted old VM", "vmId", vm.VMID)
	}

	zlog.InfoWithCtx(c, "Cleanup of old VMs completed", "processed", successCount)
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
		} else if param.ProxyType == constants.ProxyTypeHttpProxy || param.ProxyType == "service" {
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
	vmName := fmt.Sprintf("gatcvm-%s-%s-%s", strings.ToLower(proxyType), strings.ToLower(param.Tag), time.Now().Format("20060102150405"))

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
		VMID:          vmName,
		VMName:        vmName,
		Zone:          zone,
		MachineType:   machineType,
		ExternalIP:    externalIP,
		Proxy:         proxyAuth,
		ProxyType:     proxyType,
		SSHUser:       "gatc",
		SSHKeyContent: gcpConfig.GetSSHKeyContent(),
		Status:        constants.VMStatusRunning,
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
			}, fmt.Errorf("VM not found")
		}
		return &DeleteVMResult{
			Success: false,
			Message: "Failed to query VM",
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
		}, fmt.Errorf("failed to update VM status: %v", err)
	}

	return &DeleteVMResult{
		Success: true,
		Message: "VM deleted successfully",
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
func (s *VMService) SyncVMsWithGCP() {
	c := &gin.Context{}

	zlog.InfoWithCtx(c, "Starting VM sync with GCP")

	// 获取GCP中所有VM实例 (集合A)
	gcpInstances, err := s.getGCPVMInstances(c)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to get GCP VM instances during sync", err)
		return
	}

	// 获取数据库中非已删除的记录 (集合B)
	dbInstances, err := dao.GVmInstanceDao.GetActiveVMs(c)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to get active VM instances from DB during sync", err)
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

	// B-A: 数据库中有但GCP中没有的GATC VM，设置为删除状态
	var toDeleteVMIDs []string
	for ID := range dbVMIds {
		if _, exists := gcpVMIds[ID]; !exists {
			toDeleteVMIDs = append(toDeleteVMIDs, ID)
		}
	}

	if len(toDeleteVMIDs) > 0 {
		if err := dao.GVmInstanceDao.BatchUpdateStatusDeleted(c, toDeleteVMIDs); err != nil {
			zlog.ErrorWithCtx(c, "Failed to batch delete VMs during sync", err)
		} else {
			zlog.InfoWithCtx(c, "Marked VMs as deleted during sync", "count", len(toDeleteVMIDs), "vmIds", toDeleteVMIDs)
		}
	}

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
			zlog.ErrorWithCtx(c, "Failed to insert VM during sync", err)
		} else {
			successCount++
		}
	}

	if len(toInsertVMs) > 0 {
		zlog.InfoWithCtx(c, "Inserted new GATC VMs during sync", "attempted", len(toInsertVMs), "success", successCount)
	}

	zlog.InfoWithCtx(c, "GATC VM sync with GCP completed",
		"totalGCPVMs", len(gcpInstances),
		"gatcGCPVMs", len(gcpVMIds),
		"totalDBVMs", len(dbInstances),
		"gatcDBVMs", len(dbVMIds),
		"deleted", len(toDeleteVMIDs),
		"inserted", successCount)
}
