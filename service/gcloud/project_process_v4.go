package gcloud

import (
	"fmt"
	"math/rand"
	"os/exec"
	"time"

	"gatc/base/zlog"
	"gatc/constants"
	"gatc/dao"
	"gatc/tool"
	"github.com/gin-gonic/gin"
)

// ProjectProcessV4Param V4 项目处理参数
type ProjectProcessV4Param struct {
	Email                    string `json:"email" form:"email" binding:"required"`
	MaxCreateProjNum         int    `json:"max_create_proj_num" form:"max_create_proj_num"`
	FirstTimeProcess         *bool  `json:"first_time_process,omitempty" form:"first_time_process,omitempty"`
	FirstTimeCountAsExisting *bool  `json:"first_time_count_as_existing,omitempty" form:"first_time_count_as_existing,omitempty"`
	UnbindOldBillingProj     *bool  `json:"unbind_old_billing_proj,omitempty" form:"unbind_old_billing_proj,omitempty"`
	FirstTimeExemptUnbind    *bool  `json:"first_time_exempt_unbind,omitempty" form:"first_time_exempt_unbind,omitempty"`
}

// Adapt 参数适配，设置默认值
func (p *ProjectProcessV4Param) Adapt() {
	if p.MaxCreateProjNum == 0 {
		p.MaxCreateProjNum = 3
	}
	if p.FirstTimeCountAsExisting == nil {
		p.FirstTimeCountAsExisting = tool.NewPtr(true)
	}
	if p.UnbindOldBillingProj == nil {
		p.UnbindOldBillingProj = tool.NewPtr(true)
	}
	if p.FirstTimeExemptUnbind == nil {
		p.FirstTimeExemptUnbind = tool.NewPtr(true)
	}
}

// CreateProjBillingUnbindV4Result 创建项目并解绑账单结果
type CreateProjBillingUnbindV4Result struct {
	Email                 string   `json:"email"`
	Success               bool     `json:"success"`
	Message               string   `json:"message"`
	TotalProjects         int      `json:"total_projects"`
	CreatedProjects       int      `json:"created_projects"`
	CreatedProjectsDetail []string `json:"created_projects_detail"`
	UnboundProjects       int      `json:"unbound_projects"`
	UnboundProjectsDetail []string `json:"unbound_projects_detail"`
	NeedManualBillingBind bool     `json:"need_manual_billing_bind"`
}

// KeyWithdrawV4Param 提取Key参数
type KeyWithdrawV4Param struct {
	Email      string   `json:"email" form:"email" binding:"required"`
	ProjectIDs []string `json:"project_ids,omitempty" form:"project_ids,omitempty"`
}

// KeyWithdrawKeySaveV4Result 提取Key并保存结果
type KeyWithdrawKeySaveV4Result struct {
	Email                 string   `json:"email"`
	Success               bool     `json:"success"`
	Message               string   `json:"message"`
	ProcessedProjects     int      `json:"processed_projects"`
	SuccessTokens         int      `json:"success_tokens"`
	FailedTokens          int      `json:"failed_tokens"`
	SuccessProjectsDetail []string `json:"success_projects_detail"`
	FailedProjectsDetail  []string `json:"failed_projects_detail"`
	SyncedTokens          int      `json:"synced_tokens"`
}

// CreateProjBillingUnbindV4 创建项目并解绑账单（V4）
// 使用 GCPProjectExt 携带状态（NewCreate, BillingAccount），简化逻辑
func CreateProjBillingUnbindV4(c *gin.Context, param *ProjectProcessV4Param) (*CreateProjBillingUnbindV4Result, error) {
	result := &CreateProjBillingUnbindV4Result{
		Email:                 param.Email,
		Success:               false,
		CreatedProjectsDetail: make([]string, 0),
		UnboundProjectsDetail: make([]string, 0),
		NeedManualBillingBind: true,
	}

	// 参数适配
	param.Adapt()

	zlog.InfoWithCtx(c, "CreateProjBillingUnbindV4 开始", "email", param.Email)

	// 1. 初始化 WorkCtx
	workCtx, err := initWorkCtxForV4(c, param.Email)
	if err != nil {
		result.Message = fmt.Sprintf("初始化失败: %v", err)
		return result, err
	}

	// 2. 获取 CLI 项目列表
	cliProjects, err := getCLIProjects(workCtx)
	if err != nil {
		result.Message = fmt.Sprintf("获取cli项目列表失败: %v", err)
		return result, err
	}

	// 3. 获取数据库记录，用于判断是否需要检查账单
	dbProjects, errGetDbRecords := dao.GGcpAccountDao.GetProjectsByEmail(c, param.Email)
	if errGetDbRecords != nil {
		result.Message = fmt.Sprintf("获取db项目列表失败: %v", errGetDbRecords)
		return result, errGetDbRecords
	}
	dbProjectMap := make(map[string]*dao.GCPAccount)
	for i := range dbProjects {
		dbProjectMap[dbProjects[i].ProjectID] = &dbProjects[i]
	}

	// 4. 检查项目账单状态（跳过 DB 中已解绑的）
	for i := range cliProjects {
		dbProject := dbProjectMap[cliProjects[i].ProjectID]
		// 如果 DB 中不存在或不是解绑状态，才检查实际状态
		if dbProject == nil || dbProject.BillingStatus != dao.BillingStatusDetach {
			billingAccount, errCheckBilling := getProjectBillingAccount(workCtx, cliProjects[i].ProjectID)
			if errCheckBilling != nil {
				result.Message = fmt.Sprintf("cli检查项目账单状态失败: %v", errCheckBilling)
				return result, errCheckBilling
			}
			cliProjects[i].BillingAccount = billingAccount // 空字符串表示未绑定
		}
	}

	// 自动识别首次处理
	isFirstTime := false
	if param.FirstTimeProcess == nil {
		isFirstTime = len(cliProjects) <= 1
	} else {
		isFirstTime = *param.FirstTimeProcess
	}

	zlog.InfoWithCtx(c, "CreateProjBillingUnbindV4 项目状态", "当前项目数", len(cliProjects), "是否首次", isFirstTime)

	// 5. 计算并创建新项目
	needCreateCount := 0
	if isFirstTime && param.FirstTimeCountAsExisting != nil && *param.FirstTimeCountAsExisting {
		needCreateCount = max(0, param.MaxCreateProjNum-len(cliProjects))
	} else {
		needCreateCount = param.MaxCreateProjNum
	}

	if needCreateCount > 0 {
		createdProjectIDs, err := createProjects(workCtx, needCreateCount)
		if err != nil {
			zlog.ErrorWithMsgAndCtx(c, "CreateProjBillingUnbindV4 创建项目失败", err)
			result.Message = fmt.Sprintf("创建项目失败: %v", err)
			return result, err
		}
		result.CreatedProjects = len(createdProjectIDs)
		result.CreatedProjectsDetail = createdProjectIDs

		// 添加新建的项目到列表（标记 NewCreate=true，BillingAccount 为空）
		for _, pid := range createdProjectIDs {
			cliProjects = append(cliProjects, GCPProject{
				ProjectID: pid,
				Name:      "GATC Project",
				NewCreate: true,
			})
		}
	}

	result.TotalProjects = len(cliProjects)

	// 6. 同步所有项目到 DB（使用内存中准确的 billing 状态）
	if err = syncProjectsToDBV4(workCtx, cliProjects, dbProjectMap); err != nil {
		zlog.ErrorWithMsgAndCtx(c, "CreateProjBillingUnbindV4 同步项目到数据库失败", err)
		result.Message = fmt.Sprintf("同步db到数据库失败: %v", err)
		return result, err
	}

	// 7. 按需解绑（简化：直接遍历 BillingAccount 不为空且非新建的项目）
	shouldUnbind := param.UnbindOldBillingProj != nil && *param.UnbindOldBillingProj
	if shouldUnbind && isFirstTime && param.FirstTimeExemptUnbind != nil && *param.FirstTimeExemptUnbind {
		shouldUnbind = false
		zlog.InfoWithCtx(c, "CreateProjBillingUnbindV4 首次处理，豁免解绑")
	}

	if shouldUnbind { //解绑失败不直接导致流程失败
		for _, proj := range cliProjects {
			// 跳过新建项目（BillingAccount 肯定为空）和已解绑项目
			if proj.NewCreate || proj.BillingAccount == "" {
				continue
			}
			// 执行解绑
			if err := unbindProjectBilling(workCtx, proj.ProjectID); err != nil {
				zlog.ErrorWithMsgAndCtx(c, "CreateProjBillingUnbindV4 解绑账单失败", err, "projectID", proj.ProjectID)
				continue
			}
			result.UnboundProjects++
			result.UnboundProjectsDetail = append(result.UnboundProjectsDetail, proj.ProjectID)
			// 更新 DB 状态
			err2 := updateBillingStatusToDetach(c, param.Email, proj.ProjectID)
			if err2 != nil {
				zlog.ErrorWithMsgAndCtx(c, "CreateProjBillingUnbindV4 解绑账单更新db失败", err, "projectID", proj.ProjectID)
			}
			zlog.InfoWithCtx(c, "CreateProjBillingUnbindV4 解绑账单成功", "projectID", proj.ProjectID)
		}
	}

	result.Success = true
	result.Message = fmt.Sprintf("完成: 总项目%d, 新建%d, 解绑%d",
		result.TotalProjects, result.CreatedProjects, result.UnboundProjects)

	zlog.InfoWithCtx(c, "CreateProjBillingUnbindV4 完成", "result", result.Message)
	return result, nil
}

// getProjectBillingAccount 获取项目的账单账户，空字符串表示未绑定
func getProjectBillingAccount(workCtx *WorkCtx, projectID string) (string, error) {
	cmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", workCtx.VMInstance.SSHUser, workCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud billing projects describe %s --format='value(billingAccountName)' 2>/dev/null || echo ''", projectID),
	)

	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// syncProjectsToDBV4 同步项目到数据库（V4 版本，使用准确的 billing 状态）
func syncProjectsToDBV4(workCtx *WorkCtx, projects []GCPProject, dbProjectMap map[string]*dao.GCPAccount) error {
	for _, proj := range projects {
		db, ok := dbProjectMap[proj.ProjectID]
		// 判断 billing 状态：BillingAccount 不为空表示已绑定
		cli_billingStatus := dao.BillingStatusUnbound
		if proj.BillingAccount != "" {
			cli_billingStatus = dao.BillingStatusBound
		}
		if !ok { // 创建
			account := &dao.GCPAccount{
				Email:         workCtx.Email,
				ProjectID:     proj.ProjectID,
				BillingStatus: cli_billingStatus,
				TokenStatus:   dao.TokenStatusNone,
				VMID:          workCtx.VMInstance.VMID,
				Sock5Proxy:    workCtx.VMInstance.Proxy,
				Region:        "us-central1",
				AuthStatus:    1,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}
			if err := dao.GGcpAccountDao.Create(workCtx.GinCtx, account); err != nil {
				zlog.ErrorWithMsgAndCtx(workCtx.GinCtx, "创建项目记录失败", err, "projectID", proj.ProjectID)
				err = fmt.Errorf("syncProjectsToDB_create_proj_err %v", err)
				return err
			}
			continue
		}

		if db.BillingStatus != cli_billingStatus {
			db.BillingStatus = cli_billingStatus
			db.UpdatedAt = time.Now()
			if err := dao.GGcpAccountDao.Save(workCtx.GinCtx, db); err != nil {
				zlog.ErrorWithMsgAndCtx(workCtx.GinCtx, "更新项目账单状态失败", err, "projectID", proj.ProjectID)
				err = fmt.Errorf("syncProjectsToDB_update_proj_billingStatus err %v", err)
				return err
			}
		}

	}
	return nil
}

// KeyWithdrawKeySaveV4 提取Key并保存（V4）
// 添加了异步同步机制：如果提取失败或提取数不足，会自动触发同步
func KeyWithdrawKeySaveV4(c *gin.Context, param *KeyWithdrawV4Param) (*KeyWithdrawKeySaveV4Result, error) {
	result := &KeyWithdrawKeySaveV4Result{
		Email:                 param.Email,
		Success:               false,
		SuccessProjectsDetail: make([]string, 0),
		FailedProjectsDetail:  make([]string, 0),
	}

	zlog.InfoWithCtx(c, "KeyWithdrawKeySaveV4 开始：提取Key并保存", "email", param.Email)

	// 1. 初始化 WorkCtx
	workCtx, err := initWorkCtxForV4(c, param.Email)
	if err != nil {
		result.Message = fmt.Sprintf("初始化失败: %v", err)
		return result, err
	}

	// 2. 获取符合条件的项目
	projects, err := getProjectsForKeyWithdraw(c, param.Email, param.ProjectIDs)
	if err != nil {
		result.Message = fmt.Sprintf("获取项目失败: %v", err)
		return result, err
	}

	result.ProcessedProjects = len(projects)

	// 3. 遍历项目提取Key
	for _, project := range projects {
		zlog.InfoWithCtx(c, "KeyWithdrawKeySaveV4 开始提取Key", "projectID", project.ProjectID)

		success, token := generateTokenForProject(workCtx, project.ProjectID)
		if success && token != "" {
			// 更新数据库
			project.TokenStatus = dao.TokenStatusGot
			project.OfficialToken = token
			project.UpdatedAt = time.Now()

			if err := dao.GGcpAccountDao.Save(c, &project); err != nil {
				zlog.ErrorWithMsgAndCtx(c, "KeyWithdrawKeySaveV4 保存Token失败", err, "projectID", project.ProjectID)
				result.FailedTokens++
				result.FailedProjectsDetail = append(result.FailedProjectsDetail, project.ProjectID)
			} else {
				result.SuccessTokens++
				result.SuccessProjectsDetail = append(result.SuccessProjectsDetail, project.ProjectID)
				zlog.InfoWithCtx(c, "KeyWithdrawKeySaveV4 提取Key成功", "projectID", project.ProjectID)
			}
		} else {
			// 更新失败状态
			project.TokenStatus = dao.TokenStatusCreateFail
			project.UpdatedAt = time.Now()
			dao.GGcpAccountDao.Save(c, &project)

			result.FailedTokens++
			result.FailedProjectsDetail = append(result.FailedProjectsDetail, project.ProjectID)
			zlog.InfoWithCtx(c, "KeyWithdrawKeySaveV4 提取Key失败", "projectID", project.ProjectID)
		}
	}

	// 4. 同步到 official_tokens
	if result.SuccessTokens > 0 {
		// 构建 PostLoginProcessCtx 用于同步
		ctx := &PostLoginProcessCtx{
			Ctx: &WorkCtx{GinCtx: c, Email: param.Email},
		}
		synced, err := PostLoginProcessStep5TokenSync(ctx)
		if err != nil {
			zlog.ErrorWithMsgAndCtx(c, "KeyWithdrawKeySaveV4 同步token失败", err)
			result.Message = fmt.Sprintf("同步Token失败: %v", err)
			return result, err
		} else {
			result.SyncedTokens = synced
		}
	}

	result.Success = result.SuccessTokens > 0
	result.Message = fmt.Sprintf("KeyWithdrawKeySaveV4 完成: 处理%d, 成功%d, 失败%d, 同步%d",
		result.ProcessedProjects, result.SuccessTokens, result.FailedTokens, result.SyncedTokens)

	zlog.InfoWithCtx(c, "KeyWithdrawKeySaveV4 流程完成", "result", result.Message)
	return result, nil
}

// boolPtr 返回 bool 指针
func boolPtr(b bool) *bool {
	return &b
}

// initWorkCtxForV4 初始化 WorkCtx
func initWorkCtxForV4(c *gin.Context, email string) (*WorkCtx, error) {
	// 从数据库获取账号状态
	accountStatus, err := dao.GGcpAccountDao.GetAccountStatus(c, email)
	if err != nil {
		return nil, fmt.Errorf("账号状态不存在，请先登录: %s", email)
	}

	// 检查登录状态
	if accountStatus.AuthStatus != dao.AuthStatusLoggedIn {
		return nil, fmt.Errorf("账号未登录，请先登录")
	}

	// 获取VM实例
	vmInstance, err := dao.GVmInstanceDao.GetByVMID(c, accountStatus.VMID)
	if err != nil || vmInstance.Status != constants.VMStatusRunning {
		return nil, fmt.Errorf("VM不存在或状态异常，请先登录，或重试开号流程")
	}

	return &WorkCtx{
		SessionID:  fmt.Sprintf("gcpv4_%d_%d", time.Now().Unix(), rand.Intn(10000)),
		Email:      email,
		VMInstance: vmInstance,
		GinCtx:     c,
	}, nil
}

// updateBillingStatusToDetach 更新账单状态为已解绑
func updateBillingStatusToDetach(c *gin.Context, email, projectID string) error {
	account, err := dao.GGcpAccountDao.GetByEmailAndProject(c, email, projectID)
	if err != nil {
		return err
	}

	account.BillingStatus = dao.BillingStatusDetach
	account.UpdatedAt = time.Now()
	return dao.GGcpAccountDao.Save(c, account)
}

// getProjectsForKeyWithdraw 获取符合条件的项目
func getProjectsForKeyWithdraw(c *gin.Context, email string, projectIDs []string) ([]dao.GCPAccount, error) {
	// 如果指定了项目ID，直接查询这些项目
	if len(projectIDs) > 0 {
		var result []dao.GCPAccount
		for _, pid := range projectIDs {
			account, err := dao.GGcpAccountDao.GetByEmailAndProject(c, email, pid)
			if err == nil {
				result = append(result, *account)
			}
		}
		return result, nil
	}

	// 否则查询所有符合条件的项目：
	// - 已绑定账单（billing_status = 1）
	// - 未解绑（billing_status != 2）
	// - 没有key（token_status = 0）
	allProjects, err := dao.GGcpAccountDao.GetProjectsByEmail(c, email)
	if err != nil {
		return nil, err
	}

	var result []dao.GCPAccount
	for _, p := range allProjects {
		if p.BillingStatus == dao.BillingStatusBound &&
			p.TokenStatus == dao.TokenStatusNone {
			result = append(result, p)
		}
	}

	return result, nil
}
