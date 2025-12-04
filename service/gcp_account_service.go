package service

import (
	"errors"
	"fmt"
	"gatc/base/config"
	"gatc/base/zlog"
	"gatc/constants"
	"gatc/dao"
	"gatc/helpers"
	"gatc/service/gcloud"
	"gatc/tool"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// StartAccountRegistrationParam 开始开号参数
type StartAccountRegistrationParam struct {
	Email     string `json:"email" form:"email"`
	ProxyType string `json:"proxy_type,omitempty"  form:"proxy_type,omitempty"`
}

// StartAccountRegistrationResult 开始开号返回结果
type StartAccountRegistrationResult struct {
	SessionID   string `json:"session_id"`
	Email       string `json:"email"`
	LoginURL    string `json:"login_url"`
	CallbackURL string `json:"callback_url"`
	VMID        string `json:"vm_id"`
	Msg         string `json:"msg"`
}

// SubmitAuthKeyParam 提交验证密钥参数
type SubmitAuthKeyParam struct {
	SessionID string `json:"session_id"`
	AuthKey   string `json:"auth_key"`
}

// SubmitAuthKeyResult 提交验证密钥返回结果
type SubmitAuthKeyResult struct {
	SessionID string `json:"session_id"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Email     string `json:"email,omitempty"`
}

// ListAccountParam 查询账户列表参数
type ListAccountParam struct {
	Status int `json:"status,omitempty" form:"status"`
	Page   int `json:"page,omitempty" form:"page"`
	Size   int `json:"size,omitempty" form:"size"`
}

// ListAccountResult 查询账户列表返回结果
type ListAccountResult struct {
	Total int64            `json:"total"`
	Page  int              `json:"page"`
	Size  int              `json:"size"`
	Items []dao.GCPAccount `json:"items"`
}

type GcpAccountService struct{}

var GGcpAccountService = &GcpAccountService{}

// StartAccountRegistration 开始账户注册流程
func (s *GcpAccountService) StartAccountRegistration(c *gin.Context, param *StartAccountRegistrationParam) (ret *StartAccountRegistrationResult, err error) {
	ret = &StartAccountRegistrationResult{}
	if param.Email == "" {
		ret.Msg = "no email"
		return nil, errors.New("no email")
	}

	ForceCreateVm := false
	needCreateVm := ForceCreateVm
	var vmInstance *dao.VMInstance
	var vmId string

	if !ForceCreateVm {
		// 查询db gcp_account 查询该邮箱的 vmId
		account, err := dao.GGcpAccountDao.GetAccountStatus(c, param.Email)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("查询账户状态失败: %v", err)
		}
		if account != nil {
			vmId = account.VMID
		}
		if errors.Is(err, gorm.ErrRecordNotFound) || vmId == "" {
			// 账户记录不存在或没有关联VM，需要创建新VM
			needCreateVm = true
			zlog.InfoWithCtx(c, "账户无VM记录，需要创建新VM", "email", param.Email)
		} else {
			// 验证现有VM是否有效
			validInstance, vmNotExists, vmInvalid, err := s.getValidVm(c, vmId)
			if err != nil {
				return nil, fmt.Errorf("验证vm状态失败 %v", err)
			}
			if vmNotExists {
				// VM记录不存在，需要创建新VM
				s.cleanAccountVmIdTag(c, vmId)
				needCreateVm = true
				zlog.InfoWithCtx(c, "VM记录不存在，需要创建新VM", "email", param.Email, "invalidVmId", vmId)
			} else if vmInvalid {
				// VM状态异常，清理标记并创建新VM
				s.cleanAccountVmIdTag(c, vmId)
				needCreateVm = true
				zlog.InfoWithCtx(c, "VM状态异常，需要创建新VM", "email", param.Email, "invalidVmId", vmId)
			} else {
				// VM有效，使用现有VM
				vmInstance = validInstance
				needCreateVm = false
				zlog.InfoWithCtx(c, "使用现有有效VM", "email", param.Email, "vmId", vmId)
			}
		}
	}

	if needCreateVm {
		// 新建VM逻辑
		zlog.InfoWithCtx(c, "创建新VM用于账户注册", "email", param.Email)
		createResult, err := GVmService.CreateVM(c, &CreateVMParam{
			ProxyType: param.ProxyType,
		})
		if err != nil {
			return nil, fmt.Errorf("创建新VM失败: %v", err)
		}

		// 获取刚创建的VM实例
		vmInstance, err = dao.GVmInstanceDao.GetByVMID(c, createResult.VMID)
		if err != nil {
			return nil, fmt.Errorf("获取新创建的VM失败: %v", err)
		}
		vmId = createResult.VMID
		zlog.InfoWithCtx(c, "新VM创建成功", "vmId", vmId, "externalIP", createResult.ExternalIP)

		// 将account表该email对应所有项目的vmId和sock5_proxy更新到新VM // 如果有
		err = s.updateAccountVMInfo(c, param.Email, vmInstance)
		if err != nil {
			zlog.ErrorWithCtx(c, "更新账户VM信息失败", err)
			// 不影响主流程，记录错误继续执行
		}
		time.Sleep(10 * time.Second)
	}

	// 生成唯一ID
	sessionID := fmt.Sprintf("sess___%d___%s___%s", time.Now().Unix(), strings.ReplaceAll(param.Email, "@", "_"), vmId)
	zlog.InfoWithCtx(c, "Starting account registration", "sessionID", sessionID)
	ret.SessionID = sessionID
	ret.Email = param.Email
	ret.VMID = vmId

	gcloudCtx := &gcloud.WorkCtx{
		SessionID:  sessionID,
		Email:      param.Email,
		VMInstance: vmInstance,
		GinCtx:     c,
	}

	var authStatus gcloud.AccountAuthStatus
	authStatus, err = gcloudCtx.CheckTargetAccount() // 可能首次创建 vm， 登录容易失败
	for i := 0; i < 6 && err != nil; i++ {
		time.Sleep(5 * time.Second)
		authStatus, err = gcloudCtx.CheckTargetAccount()
	}
	if err != nil {
		ret.Msg = "获取登陆状态失败: " + err.Error()
		zlog.InfoWithCtx(c, ret.Msg)
		return
	}

	// 2. 根据账户状态决定操作
	switch authStatus {
	case gcloud.AccountAuthSStatusActive:
		// 账户已active，写入数据库状态记录
		zlog.InfoWithCtx(gcloudCtx.GinCtx, "Target account already active for session", "sessionID", gcloudCtx.SessionID)
		err = dao.GGcpAccountDao.CreateOrUpdateAccountStatus(c, param.Email, vmInstance.VMID, dao.AuthStatusLoggedIn, "账户已登录")
		if err != nil {
			zlog.ErrorWithCtx(gcloudCtx.GinCtx, "保存账户状态失败", err)
		}
		ret.Msg = "账户已登录"
		return

	case gcloud.AccountAuthStatusInactive:
		// 有账户但非active，切换账户
		if err = gcloudCtx.SwitchToAccount(); err != nil {
			zlog.ErrorWithCtx(gcloudCtx.GinCtx, "Failed to switch account for session sessionID:"+gcloudCtx.SessionID, err)
			ret.Msg = fmt.Sprintf("账户已存在，切换账户失败  %v", err)
			err = dao.GGcpAccountDao.CreateOrUpdateAccountStatus(c, param.Email, vmInstance.VMID, dao.AuthStatusLoginFailed, "切换账户失败: "+err.Error())
			if err != nil {
				zlog.ErrorWithCtx(gcloudCtx.GinCtx, "保存账户状态失败", err)
			}
			return
		} else {
			zlog.InfoWithCtx(gcloudCtx.GinCtx, "账户已存在， 直接切换成功", "sessionID", gcloudCtx.SessionID)
			err = dao.GGcpAccountDao.CreateOrUpdateAccountStatus(c, param.Email, vmInstance.VMID, dao.AuthStatusLoggedIn, "账户切换成功")
			if err != nil {
				zlog.ErrorWithCtx(gcloudCtx.GinCtx, "保存账户状态失败", err)
			}
			ret.Msg = "账户已存在，切换成功，已登陆"
			return
		}

	case gcloud.AccountAuthSStatusNotLogin:
		zlog.InfoWithCtx(gcloudCtx.GinCtx, "账户不存在， 需要登录", "sessionID", gcloudCtx.SessionID)
		// 无目标账户，需要登录
	}

	// 创建登录会话
	authSession, err := gcloud.NewAuthLoginSession(gcloudCtx)
	if err != nil {
		zlog.ErrorWithCtx(gcloudCtx.GinCtx, "Failed to NewAuthLoginSession:"+gcloudCtx.SessionID, err)
		ret.Msg = fmt.Sprintf("创建登陆会话失败  %v", err)
		return
	}

	ret.LoginURL, err = authSession.DoLogin()
	if err != nil {
		zlog.ErrorWithCtx(gcloudCtx.GinCtx, "Failed to get loginUrl:"+gcloudCtx.SessionID, err)
		ret.Msg = fmt.Sprintf("获取登陆url失败  %v", err)
		return
	}

	// 登录阶段暂不写入数据库，只有获得project后才写入

	// 只在有登录URL时才提供回调URL

	if ret.LoginURL != "" {
		// 构造完整的回调URL (实际部署时应该用真实域名)
		ret.CallbackURL = fmt.Sprintf("http://localhost:5401/api/v1/account/submit-auth-key?session_id=%s&auth_key={填写用户登录拿到的key}", sessionID)
	}

	return
}

// SubmitAuthKey 提交验证密钥完成登录
func (s *GcpAccountService) SubmitAuthKey(c *gin.Context, param *SubmitAuthKeyParam) (*SubmitAuthKeyResult, error) {
	zlog.InfoWithCtx(c, "Submitting auth key", "sessionID", param.SessionID)

	// 从sessionID中提取email信息
	email := s.extractEmailFromSessionID(param.SessionID)
	if email == "" {
		return &SubmitAuthKeyResult{
			SessionID: param.SessionID,
			Success:   false,
			Message:   "无效的会话ID, 解析email失败",
		}, nil
	}

	session, exist := gcloud.GAuthSessionSessionCache.GetAuthSession(param.SessionID)
	if !exist {
		return &SubmitAuthKeyResult{
			SessionID: param.SessionID,
			Success:   false,
			Message:   "会话ID不存在",
		}, nil
	}

	err := session.CompleteLoginToken(param.AuthKey)
	if err != nil {
		return &SubmitAuthKeyResult{
			SessionID: param.SessionID,
			Success:   false,
			Message:   "回填登陆Key失败：" + err.Error(),
		}, err
	}

	status, err := session.Ctx.CheckTargetAccount()
	if err != nil {
		return &SubmitAuthKeyResult{
			SessionID: param.SessionID,
			Success:   false,
			Message:   "效验登陆结果失败 " + err.Error(),
		}, err
	}

	if status == gcloud.AccountAuthSStatusActive {
		// 登录成功，写入数据库状态记录
		zlog.InfoWithCtx(c, "登录验证成功", "sessionID", param.SessionID, "email", email)

		// 保存账户状态到数据库
		err = dao.GGcpAccountDao.CreateOrUpdateAccountStatus(c, email, session.Ctx.VMInstance.VMID, dao.AuthStatusLoggedIn, "登录成功")
		if err != nil {
			zlog.ErrorWithCtx(c, "保存账户状态失败", err)
		}

		return &SubmitAuthKeyResult{
			SessionID: param.SessionID,
			Success:   true,
			Message:   "登录成功",
			Email:     email,
		}, nil
	}

	return &SubmitAuthKeyResult{
		SessionID: param.SessionID,
		Success:   false,
		Message:   "登陆状态异常" + string(status),
	}, nil
}

// extractEmailFromSessionID 从会话ID中提取邮箱
func (s *GcpAccountService) extractEmailFromSessionID(sessionID string) string {
	parts := strings.Split(sessionID, "___")
	if len(parts) >= 4 {
		return strings.ReplaceAll(parts[2], "_", "@")
	}
	return ""
}

// extractVMIDFromSessionID 从会话ID中提取VM ID
func (s *GcpAccountService) extractVMIDFromSessionID(sessionID string) string {
	parts := strings.Split(sessionID, "___")
	if len(parts) >= 4 {
		return parts[3] // VM ID是第4部分
	}
	return ""
}

// ListAccounts 查询账户列表
func (s *GcpAccountService) ListAccounts(c *gin.Context, param *ListAccountParam) (*ListAccountResult, error) {
	page := 1
	if param.Page > 0 {
		page = param.Page
	}

	size := 10
	if param.Size > 0 && param.Size <= 100 {
		size = param.Size
	}

	offset := (page - 1) * size

	items, total, err := dao.GGcpAccountDao.List(c, param.Status, offset, size)
	if err != nil {
		return nil, fmt.Errorf("查询账户列表失败: %v", err)
	}

	return &ListAccountResult{
		Total: total,
		Page:  page,
		Size:  size,
		Items: items,
	}, nil
}

// getValidVm 验证VM是否有效
// 返回值：vmInstance, vmNotExists, vmInValid
func (s *GcpAccountService) getValidVm(c *gin.Context, vmID string) (*dao.VMInstance, bool, bool, error) {
	if vmID == "" {
		return nil, false, false, errors.New("no vimId")
	}

	// 查询VM记录
	vmInstance, err := dao.GVmInstanceDao.GetByVMID(c, vmID)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			zlog.InfoWithCtx(c, "VM记录不存在", "vmId", vmID)
			return nil, true, false, nil
		}
		zlog.ErrorWithCtx(c, "查询VM记录失败", err)
		return nil, false, false, err
	}

	// 检查VM状态
	if vmInstance.Status != constants.VMStatusRunning {
		zlog.InfoWithCtx(c, "VM状态不是运行状态", "vmId", vmID, "status", vmInstance.Status)
		return vmInstance, false, true, nil
	}

	// 通过gcloud cli确认VM是否真实存在
	if !s.verifyVMExistsInGCP(c, vmInstance) {
		zlog.InfoWithCtx(c, "VM在GCP中不存在", "vmId", vmID)
		return vmInstance, false, true, nil
	}

	return vmInstance, false, false, nil
}

// cleanAccountVmIdTag 清理账户表中无效的VM ID标记
func (s *GcpAccountService) cleanAccountVmIdTag(c *gin.Context, invalidVmID string) {
	if invalidVmID == "" {
		return
	}

	// 更新VM状态为异常（如果记录存在的话）
	err := dao.GVmInstanceDao.UpdateStatus(c, invalidVmID, constants.VMStatusDeleted)
	if err != nil && err != gorm.ErrRecordNotFound {
		zlog.ErrorWithCtx(c, "更新VM状态为删除失败", err)
	}

	// 将gcp_accounts表中关联该VM ID的记录的vm_id置空
	err = helpers.GatcDbClient.WithContext(c).Model(&dao.GCPAccount{}).
		Where("vm_id = ?", invalidVmID).
		Update("vm_id", "").Error

	if err != nil {
		zlog.ErrorWithCtx(c, "清理账户VM ID标记失败", err)
	} else {
		zlog.InfoWithCtx(c, "已清理账户VM ID标记", "vmId", invalidVmID)
	}
}

// verifyVMExistsInGCP 通过gcloud cli验证VM是否在GCP中真实存在
func (s *GcpAccountService) verifyVMExistsInGCP(c *gin.Context, vmInstance *dao.VMInstance) bool {
	if vmInstance == nil || vmInstance.VMID == "" {
		return false
	}

	// 从全局配置获取项目ID
	gcpConfig := config.GetGCPConfig()
	if gcpConfig == nil {
		zlog.ErrorWithCtx(c, "GCP配置未初始化", nil)
		return false
	}

	projectID := gcpConfig.GetProjectID()

	// 使用gcloud compute instances describe命令检查VM是否存在
	cmdStr := fmt.Sprintf("gcloud compute instances describe %s --project=%s --zone=%s --format='value(name)'",
		vmInstance.VMID, projectID, vmInstance.Zone)

	zlog.InfoWithCtx(c, "验证VM在GCP中是否存在", "vmId", vmInstance.VMID, "command", cmdStr)

	stdout, stderr, _, err := tool.ExecCommand(cmdStr)

	// 如果命令执行失败或退出码不为0，说明VM不存在
	if err != nil {
		if strings.Contains(stderr, "was not found") || strings.Contains(stderr, "does not exist") {
			zlog.InfoWithCtx(c, "VM在GCP中不存在", "vmId", vmInstance.VMID, "stderr", stderr)
		} else {
			zlog.ErrorWithCtx(c, "验证VM存在性失败", fmt.Errorf("command failed: %v, stderr: %s", err, stderr))
		}
		return false
	}

	// 检查输出是否包含VM名称
	vmName := strings.TrimSpace(stdout)
	if vmName == vmInstance.VMID || vmName == vmInstance.VMName {
		zlog.InfoWithCtx(c, "VM在GCP中存在并有效", "vmId", vmInstance.VMID)
		return true
	}

	zlog.InfoWithCtx(c, "VM验证结果异常", "vmId", vmInstance.VMID, "output", vmName)
	return false
}

// updateAccountVMInfo 更新账户表中该邮箱对应所有记录的VM信息
func (s *GcpAccountService) updateAccountVMInfo(c *gin.Context, email string, newVMInstance *dao.VMInstance) error {
	if email == "" || newVMInstance == nil {
		return fmt.Errorf("email或VM实例为空")
	}

	// 使用新的代理地址（包含用户名密码）
	newSock5Proxy := newVMInstance.Proxy

	zlog.InfoWithCtx(c, "开始更新账户VM信息", "email", email, "newVmId", newVMInstance.VMID, "newSock5Proxy", newSock5Proxy)

	// 更新该邮箱下所有记录的VM ID和sock5代理信息
	result := helpers.GatcDbClient.WithContext(c).Model(&dao.GCPAccount{}).
		Where("email = ?", email).
		Updates(map[string]interface{}{
			"vm_id":       newVMInstance.VMID,
			"sock5_proxy": newSock5Proxy,
			"updated_at":  time.Now(),
		})

	if result.Error != nil {
		return fmt.Errorf("更新账户VM信息失败: %v", result.Error)
	}

	zlog.InfoWithCtx(c, "账户VM信息更新完成", "email", email, "updatedRows", result.RowsAffected, "newVmId", newVMInstance.VMID)

	return nil
}
