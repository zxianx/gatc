package gcloud

import (
	"context"
	"encoding/json"
	"fmt"
	"gatc/base/zlog"
	"gatc/constants"
	"gatc/dao"
	"gatc/helpers"
	"github.com/gin-gonic/gin"
	"os/exec"
	"strings"
	"time"
)

// ProjectProcessCtx 项目处理上下文
type ProjectProcessCtx struct {
	baseCtx        *WorkCtx
	email          string
	billingAccount string
}

// ProjectProcessParam 项目处理参数
type ProjectProcessParam struct {
	Email                string `json:"email" form:"email" binding:"required"`
	UnbindOldBillingProj *bool  `json:"unbind_old_billing_proj,omitempty"  form:"unbind_old_billing_proj,omitempty"`
}

// ProjectProcessResult 项目处理结果
type ProjectProcessResult struct {
	Email                 string   `json:"email"`
	Success               bool     `json:"success"`
	Message               string   `json:"message"`
	SyncedProjects        int      `json:"synced_projects"` // 同步的项目数
	SyncedProjectsDetail  []string `json:"synced_projects_detail"`
	CreatedProjects       int      `json:"created_projects"` // 新创建的项目数
	CreatedProjectsDetail []string `json:"created_projects_detail"`
	OldBindingProjects    int      `json:"old_binding_projects"`
	UnboundProjects       int      `json:"unbound_proj"`
	UnboundProjectsDetail []string `json:"unbound_proj_detail"`
	BoundProjects         int      `json:"bound_projects"` // 绑卡成功的项目数
	BoundProjectsDetail   []string `json:"bound_projects_detail"`
	CreateTokens          int      `json:"create_tokens"`
	TotalProjects         int      `json:"total_projects"` // 总项目数
	SyncedTokens          int      `json:"synced_tokens"`  // 同步到official_tokens的token数
}

// NewProjectProcessCtx 创建项目处理上下文
func NewProjectProcessCtx(c *gin.Context, email string) (*ProjectProcessCtx, error) {
	// 从数据库获取账号状态
	accountStatus, err := dao.GGcpAccountDao.GetAccountStatus(nil, email)
	if err != nil {
		return nil, fmt.Errorf("账号状态不存在，请先登录: %s", email)
	}

	// 检查登录状态
	if accountStatus.AuthStatus != dao.AuthStatusLoggedIn {
		// 更新状态为需要重新登录
		dao.GGcpAccountDao.CreateOrUpdateAccountStatus(nil, email, accountStatus.VMID, dao.AuthStatusNotLogin, "需要重新登录")
		return nil, fmt.Errorf("账号未登录，请先完成登录流程")
	}

	// 获取VM实例
	vmInstance, err := dao.GVmInstanceDao.GetByVMID(nil, accountStatus.VMID)
	if err != nil || vmInstance.Status != constants.VMStatusRunning {
		// VM异常，更新状态
		dao.GGcpAccountDao.CreateOrUpdateAccountStatus(nil, email, accountStatus.VMID, dao.AuthStatusVMError, "VM不存在或状态异常")
		return nil, fmt.Errorf("VM不存在或状态异常，请检查VM状态")
	}

	// 创建WorkCtx
	baseCtx := &WorkCtx{
		SessionID:  fmt.Sprintf("process_%d_%s", time.Now().Unix(), strings.ReplaceAll(email, "@", "_")),
		Email:      email,
		VMInstance: vmInstance,
		GinCtx:     c,
	}

	return &ProjectProcessCtx{
		baseCtx:        baseCtx,
		email:          email,
		billingAccount: "", // 将在处理过程中自动获取
	}, nil
}

// getAvailableBillingAccount 获取可用的billing account
func (ctx *ProjectProcessCtx) getAvailableBillingAccount() (string, error) {
	// 先查看是否已经有绑定了billing的项目，复用相同的billing account
	existingProjects, err := dao.GGcpAccountDao.GetProjectsByEmail(ctx.baseCtx.GinCtx, ctx.email)
	if err == nil && len(existingProjects) > 0 {
		// 查找已绑定billing的项目
		for _, project := range existingProjects {
			if project.BillingStatus == dao.BillingStatusBound && project.ProjectID != "" {
				// 通过gcloud命令获取该项目的billing account
				cmd := exec.CommandContext(context.Background(),
					"ssh", "-i", constants.SSHKeyPath,
					"-o", "StrictHostKeyChecking=no",
					"-o", "UserKnownHostsFile=/dev/null",
					fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
					fmt.Sprintf("gcloud billing projects describe %s --format='value(billingAccountName)'", project.ProjectID),
				)

				output, err := cmd.Output()
				if err == nil && len(output) > 0 {
					billingAccount := strings.TrimSpace(string(output))
					if billingAccount != "" {
						// 提取billing account ID (格式: billingAccounts/123456-ABCDEF-123456)
						if strings.HasPrefix(billingAccount, "billingAccounts/") {
							billingID := strings.TrimPrefix(billingAccount, "billingAccounts/")
							zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "从已有项目获取到billing account", "billingAccount", billingID)
							return billingID, nil
						}
					}
				}
			}
		}
	}

	// 如果没有找到已有的billing account，获取第一个可用的
	cmd := exec.CommandContext(context.Background(),
		"ssh", "-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
		"gcloud billing accounts list --filter='open=true' --format='value(name)' | head -n 1",
	)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("获取billing account失败: %v", err)
	}

	billingAccount := strings.TrimSpace(string(output))
	if billingAccount == "" {
		return "", fmt.Errorf("未找到可用的billing account")
	}

	// 提取billing account ID
	if strings.HasPrefix(billingAccount, "billingAccounts/") {
		billingID := strings.TrimPrefix(billingAccount, "billingAccounts/")
		zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "获取到第一个可用billing account", "billingAccount", billingID)
		return billingID, nil
	}

	return "", fmt.Errorf("billing account格式异常: %s", billingAccount)
}

// syncProjectsToDB 同步项目到数据库，同时检查billing和token状态
func (ctx *ProjectProcessCtx) syncProjectsToDB(projects []GCPProject) error {
	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "开始同步项目并检查状态", "项目数量", len(projects))

	for _, project := range projects {
		// 检查项目是否已存在
		existingAccount, err := dao.GGcpAccountDao.GetByEmailAndProject(ctx.baseCtx.GinCtx, ctx.baseCtx.Email, project.ProjectID)
		if err != nil && err.Error() != "record not found" {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "查询项目失败", err)
			continue
		}

		// 检查项目的billing状态
		billingStatus, billingAccount := ctx.checkProjectBillingStatus(project.ProjectID)

		// 检查项目是否有token（仅当已绑卡时检查）
		var projectStatus int
		var officialToken string

		if billingStatus == dao.BillingStatusBound {
			hasToken, token := ctx.checkProjectHasToken(project.ProjectID)
			if hasToken {
				projectStatus = dao.TokenStatusGot
				officialToken = token
			} else {
				projectStatus = dao.TokenStatusNone
			}
		} else {
			projectStatus = dao.TokenStatusNone
		}

		if existingAccount == nil {
			// 项目不存在，创建新记录
			account := &dao.GCPAccount{
				Email:         ctx.baseCtx.Email,
				ProjectID:     project.ProjectID,
				BillingStatus: billingStatus,
				TokenStatus:   projectStatus,
				VMID:          ctx.baseCtx.VMInstance.VMID,
				Region:        "us-central1",
				OfficialToken: officialToken,
				AuthStatus:    1,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}

			if err := dao.GGcpAccountDao.Create(ctx.baseCtx.GinCtx, account); err != nil {
				zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "创建项目记录失败", err)
				return fmt.Errorf("failed to create project record: %v", err)
			}

			zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "同步新项目", "项目ID", project.ProjectID,
				"billing状态", billingStatus, "项目状态", projectStatus)
		} else {
			// 项目存在，更新状态信息
			needUpdate := false

			if existingAccount.BillingStatus != billingStatus {
				existingAccount.BillingStatus = billingStatus
				needUpdate = true
			}

			if existingAccount.TokenStatus != projectStatus {
				existingAccount.TokenStatus = projectStatus
				needUpdate = true
			}

			if existingAccount.OfficialToken != officialToken {
				existingAccount.OfficialToken = officialToken
				needUpdate = true
			}

			if needUpdate {
				existingAccount.UpdatedAt = time.Now()
				if err := dao.GGcpAccountDao.Save(ctx.baseCtx.GinCtx, existingAccount); err != nil {
					zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "更新项目状态失败", err)
				} else {
					zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "更新项目状态", "项目ID", project.ProjectID,
						"billing状态", billingStatus, "项目状态", projectStatus)
				}
			}
		}

		// 记录billing account用于后续绑卡
		if billingAccount != "" && ctx.billingAccount == "" {
			ctx.billingAccount = billingAccount
			zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "从项目获取到billing account", "billingAccount", billingAccount)
		}
	}

	return nil
}

// createProjects 创建新项目，尽量补全到12个
func (ctx *ProjectProcessCtx) createProjects(currentCount int, targetCount int) ([]string, error) {
	if targetCount > 12 {
		targetCount = 12
	}

	needCount := targetCount - currentCount
	if needCount <= 0 {
		zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "项目数量已足够", "当前数量", currentCount, "目标数量", targetCount)
		return []string{}, nil
	}

	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "开始创建新项目", "需要创建", needCount)

	var createdProjects []string
	timestamp := time.Now().Unix()

	for i := 0; i < needCount; i++ {
		projectID := fmt.Sprintf("gatc-project-%d-%d", timestamp, i+1)

		// 创建项目
		cmd := exec.Command(
			"ssh",
			"-i", constants.SSHKeyPath,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
			fmt.Sprintf("gcloud projects create %s --name='GATC Project %d'", projectID, i+1),
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "创建项目失败", fmt.Errorf("项目: %s, 错误: %v, 输出: %s", projectID, err, string(output)))
			continue
		}

		zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "项目创建成功", "项目ID", projectID)
		createdProjects = append(createdProjects, projectID)

		// 创建对应的数据库记录
		account := &dao.GCPAccount{
			Email:         ctx.baseCtx.Email,
			ProjectID:     projectID,
			BillingStatus: dao.BillingStatusUnbound, // 新项目默认未绑卡
			TokenStatus:   dao.TokenStatusNone,      // 新项目默认无token
			VMID:          ctx.baseCtx.VMInstance.VMID,
			Region:        "us-central1",
			AuthStatus:    1, // 认证成功状态
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}

		if err := dao.GGcpAccountDao.Create(ctx.baseCtx.GinCtx, account); err != nil {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "创建项目数据库记录失败", err)
		}

		// 短暂等待，避免创建过快
		time.Sleep(2 * time.Second)
	}

	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "项目创建完成", "成功创建", len(createdProjects))
	return createdProjects, nil
}

// enableBillingForProjects 为未绑卡项目尝试绑卡
func (ctx *ProjectProcessCtx) enableBillingForProjects() error {
	// 获取该邮箱下所有未绑卡的项目
	unboundProjects, err := dao.GGcpAccountDao.GetUnboundProjectsByEmail(ctx.baseCtx.GinCtx, ctx.baseCtx.Email)
	if err != nil {
		return fmt.Errorf("failed to get unbound projects: %v", err)
	}

	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "开始为项目绑卡", "未绑卡项目数", len(unboundProjects))

	for _, project := range unboundProjects {
		if err := ctx.enableBillingForProject(project.ProjectID); err != nil {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "项目绑卡失败", fmt.Errorf("项目: %s, 错误: %v", project.ProjectID, err))
			continue
		}

		// 更新数据库状态
		project.BillingStatus = dao.BillingStatusBound
		// 绑卡不影响token状态，移除此设置
		project.UpdatedAt = time.Now()

		if err := dao.GGcpAccountDao.Save(ctx.baseCtx.GinCtx, &project); err != nil {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "更新项目绑卡状态失败", err)
		}

		zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "项目绑卡成功", "项目ID", project.ProjectID)
	}

	return nil
}

// enableBillingForProject 为单个项目启用计费
func (ctx *ProjectProcessCtx) enableBillingForProject(projectID string) error {
	cmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud beta billing projects link %s --billing-account=%s", projectID, ctx.billingAccount),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to enable billing: %v, output: %s", err, string(output))
	}

	return nil
}

// createGeminiTokens 为新绑卡项目创建Gemini API Token
func (ctx *ProjectProcessCtx) createGeminiTokens() error {
	// 获取新绑卡但还没有token的项目
	projects, err := dao.GGcpAccountDao.GetBoundProjectsWithoutToken(ctx.baseCtx.GinCtx, ctx.baseCtx.Email)
	if err != nil {
		return fmt.Errorf("failed to get projects without token: %v", err)
	}

	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "开始为项目创建Gemini Token", "项目数", len(projects))

	for _, project := range projects {
		token, err := ctx.createGeminiTokenForProject(project.ProjectID)
		if err != nil {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "创建Gemini Token失败", fmt.Errorf("项目: %s, 错误: %v", project.ProjectID, err))
			continue
		}

		// 更新数据库中的token
		project.OfficialToken = token
		project.UpdatedAt = time.Now()

		if err := dao.GGcpAccountDao.Save(ctx.baseCtx.GinCtx, &project); err != nil {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, "保存Gemini Token失败", err)
			continue
		}

		zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "Gemini Token创建成功", "项目ID", project.ProjectID)
	}

	return nil
}

// createGeminiTokenForProject 为单个项目创建Gemini API Token
func (ctx *ProjectProcessCtx) createGeminiTokenForProject(projectID string) (string, error) {
	// 1. 启用必要的API
	services := []string{
		"apikeys.googleapis.com",
		"generativelanguage.googleapis.com",
	}

	for _, service := range services {
		enableCmd := exec.Command(
			"ssh",
			"-i", constants.SSHKeyPath,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
			fmt.Sprintf("gcloud services enable %s --project=%s", service, projectID),
		)

		if _, err := enableCmd.CombinedOutput(); err != nil {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, fmt.Sprintf("启用服务失败: %s, 项目: %s", service, projectID), err)
		}
	}

	// 等待服务启用生效
	time.Sleep(10 * time.Second)

	// 2. 创建API Key
	createKeyCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud services api-keys create --project=%s --display-name='Gemini API Key' --api-target=service=generativelanguage.googleapis.com --format=json", projectID),
	)

	createOutput, err := createKeyCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create API key: %v, output: %s", err, string(createOutput))
	}

	// 3. 解析创建结果，提取key name
	var keyResponse struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(createOutput, &keyResponse); err != nil {
		return "", fmt.Errorf("failed to parse API key creation response: %v", err)
	}

	if keyResponse.Name == "" {
		return "", fmt.Errorf("API key name is empty in response")
	}

	// 4. 获取API key的实际token
	getKeyCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud services api-keys get-key-string %s --project=%s", keyResponse.Name, projectID),
	)

	keyOutput, err := getKeyCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get API key string: %v, output: %s", err, string(keyOutput))
	}

	apiKey := strings.TrimSpace(string(keyOutput))
	if !strings.HasPrefix(apiKey, "AIza") {
		return "", fmt.Errorf("invalid API key format: %s", apiKey)
	}

	// 5. 插入official_tokens表
	tokenId, err := ctx.insertOfficialToken(projectID, apiKey)
	if err != nil {
		zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, fmt.Sprintf("插入official_tokens表失败, 项目: %s", projectID), err)
		// 不返回错误，因为API key已经创建成功
	} else {
		// 6. 更新gcp_accounts的official_token_id
		if err := ctx.updateOfficialTokenId(projectID, tokenId); err != nil {
			zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, fmt.Sprintf("更新official_token_id失败, 项目: %s", projectID), err)
		}
	}

	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "成功创建API Key", "项目ID", projectID, "token前缀", apiKey[:10]+"...")
	return apiKey, nil
}

// checkProjectBillingStatus 检查项目的billing状态
func (ctx *ProjectProcessCtx) checkProjectBillingStatus(projectID string) (int, string) {
	cmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud billing projects describe %s --format='value(billingAccountName)'", projectID),
	)

	output, err := cmd.Output()
	if err != nil {
		zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, fmt.Sprintf("检查项目billing状态失败, 项目ID: %s", projectID), err)
		return dao.BillingStatusUnbound, ""
	}

	billingAccountName := strings.TrimSpace(string(output))
	if billingAccountName == "" || billingAccountName == "null" {
		return dao.BillingStatusUnbound, ""
	}

	// 提取billing account ID (格式: billingAccounts/123456-ABCDEF-123456)
	billingAccount := ""
	if strings.HasPrefix(billingAccountName, "billingAccounts/") {
		billingAccount = strings.TrimPrefix(billingAccountName, "billingAccounts/")
	}

	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "项目billing状态", "项目ID", projectID, "billing account", billingAccount)
	return dao.BillingStatusBound, billingAccount
}

// checkProjectHasToken 检查项目是否已有Gemini API token  
func (ctx *ProjectProcessCtx) checkProjectHasToken(projectID string) (bool, string) {
	// 先检查API Keys是否存在
	cmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud services api-keys list --project=%s --format='value(name)' --limit=1", projectID),
	)

	output, err := cmd.Output()
	if err != nil {
		zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, fmt.Sprintf("检查项目API Keys失败, 项目ID: %s", projectID), err)
		return false, ""
	}

	keyName := strings.TrimSpace(string(output))
	if keyName == "" {
		return false, ""
	}

	// 获取API key的keyString 
	getKeyCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.baseCtx.VMInstance.SSHUser, ctx.baseCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud services api-keys get-key-string %s --project=%s", keyName, projectID),
	)

	keyOutput, err := getKeyCmd.Output()
	if err != nil {
		zlog.ErrorWithCtx(ctx.baseCtx.GinCtx, fmt.Sprintf("获取API key string失败, 项目ID: %s, keyName: %s", projectID, keyName), err)
		return false, ""
	}

	keyString := strings.TrimSpace(string(keyOutput))
	if keyString != "" && strings.HasPrefix(keyString, "AIza") {
		zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "项目已有token", "项目ID", projectID, "token前缀", keyString[:10]+"...")
		return true, keyString
	}

	return false, ""
}

// insertOfficialToken 向official_tokens表插入token记录
func (ctx *ProjectProcessCtx) insertOfficialToken(projectID, apiKey string) (int64, error) {
	// 查找OfficialToken DAO的Create方法
	officialToken := &dao.GormOfficialTokens{
		ChannelId: 16,                                         // 固定16
		Name:      fmt.Sprintf("%s-%s", ctx.email, projectID), // email-projectId
		Token:     apiKey,                                     // token
		Proxy:     "",                                         // proxy socket5地址，暂时为空
		Priority:  50,                                         // priority
		Weight:    100,                                        // weight
		RpmLimit:  0,                                          // rpm_limit
		TpmLimit:  0,                                          // tpm_limit
		Status:    1,                                          // status
		TokenType: "static",                                   // token_type
		Email:     ctx.email,                                  // email
		CreatedAt: time.Now(),                                 // created_at
		UpdatedAt: time.Now(),                                 // updated_at
	}

	// 使用helpers.GatcDbClient直接插入
	if err := helpers.GatcDbClient.Create(officialToken).Error; err != nil {
		return 0, fmt.Errorf("failed to insert official_token: %v", err)
	}

	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "成功插入official_tokens记录",
		"tokenId", officialToken.Id, "项目ID", projectID, "name", officialToken.Name)

	return officialToken.Id, nil
}

// updateOfficialTokenId 更新gcp_accounts的official_token_id字段
func (ctx *ProjectProcessCtx) updateOfficialTokenId(projectID string, tokenId int64) error {
	// 获取项目记录
	account, err := dao.GGcpAccountDao.GetByEmailAndProject(ctx.baseCtx.GinCtx, ctx.email, projectID)
	if err != nil {
		return fmt.Errorf("failed to get project record: %v", err)
	}

	// 更新official_token_id
	account.OfficialTokenId = tokenId
	account.TokenStatus = dao.TokenStatusGot // 更新项目状态为已获取token
	account.UpdatedAt = time.Now()

	if err := dao.GGcpAccountDao.Save(ctx.baseCtx.GinCtx, account); err != nil {
		return fmt.Errorf("failed to update official_token_id: %v", err)
	}

	zlog.InfoWithCtx(ctx.baseCtx.GinCtx, "更新official_token_id成功",
		"项目ID", projectID, "tokenId", tokenId, "状态", dao.TokenStatusGot)

	return nil
}
