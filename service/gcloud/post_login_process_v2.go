package gcloud

import (
	"encoding/json"
	"fmt"
	"gatc/base/zlog"
	"gatc/constants"
	"gatc/dao"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GCPProjectExt 扩展的GCP项目信息
type GCPProjectExt struct {
	GCPProject
	BillingAccount string // 绑定的账单账户，nil表示还未获取
	// Ext 为了一些信息可跨step用，避免重复代码
}

// GCPProject 基础项目信息
type GCPProject struct {
	ProjectID      string `json:"projectId"`
	Name           string `json:"name"`
	ProjectNumber  string `json:"projectNumber"`
	LifecycleState string `json:"lifecycleState"`
}

// PostLoginProcessCtx 跨步骤共享的处理上下文
type PostLoginProcessCtx struct {
	Ctx             *WorkCtx                   `json:"-"`
	CliProjectList  []GCPProjectExt            `json:"cli_project_list"` // CLI获取的项目列表
	DbProjectsMp    map[string]*dao.GCPAccount `json:"db_projects_mp"`   // 数据库项目映射 projectId -> daoInstance
	BillingAccounts []string                   `json:"billing_accounts"` // 可用的billing账户列表
	Result          ProjectProcessResult       `json:"result"`           // V3新增：直接在上下文中设置结果
	UnBindCurProj   bool                       `json:"un_bind_cur_proj"` // V3新增：是否解绑当前绑定的项目
}

// ProcessPostLoginV3 执行V3的开号流程
func ProcessPostLoginV3(ctx *PostLoginProcessCtx) error {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "开始登录后处理流程V3", "邮箱", ctx.Ctx.Email)

	// 初始化Result
	ctx.Result = ProjectProcessResult{
		Email:   ctx.Ctx.Email,
		Success: false,
	}

	// Step1: 补全12个项目，同步DB（不含状态同步）
	if err := PostLoginProcessStep1ProjectSetup(ctx); err != nil {
		ctx.Result.Message = fmt.Sprintf("步骤1失败: %v", err)
		return err
	}

	// Step2: 检查billing状态，若un_bind_cur_proj=true，解绑已绑账单的项目
	if err := PostLoginProcessV3Step2BillingCheck(ctx); err != nil {
		ctx.Result.Message = fmt.Sprintf("步骤2失败: %v", err)
		return err
	}

	//Step3: 绑定billing account
	if err := PostLoginProcessV3Step3BillingBind(ctx); err != nil {
		ctx.Result.Message = fmt.Sprintf("步骤3失败: %v", err)
		return err
	}

	// 指定项目，测试
	//ctx.Result.BoundProjectsDetail = append(ctx.Result.BoundProjectsDetail, "gatc-project-1764167294-802509")
	//ctx.Result.BoundProjects = 1

	// Step4: 对新绑定billing的项目执行开token流程
	if err := PostLoginProcessV3Step4TokenGeneration(ctx); err != nil {
		ctx.Result.Message = fmt.Sprintf("步骤4失败: %v", err)
		return err
	}

	// Step5: 后置official_tokens同步
	_, err := PostLoginProcessStep5TokenSync(ctx)
	if err != nil {
		ctx.Result.Message = fmt.Sprintf("步骤5失败 PostLoginProcessStep5TokenSync: %v", err)
		return err
	}

	ctx.Result.Success = true
	ctx.Result.Message = fmt.Sprintf("V3流程完成: 项目 总计%d, 新增%d,  解绑%d，绑定%d, 提token%d，同步%d",
		ctx.Result.TotalProjects, ctx.Result.CreatedProjects, ctx.Result.UnboundProjects,
		ctx.Result.BoundProjects, ctx.Result.CreateTokens, ctx.Result.SyncedTokens)

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "登录后处理流程V3完成", "结果", ctx.Result.Message)
	return nil
}

// ProcessPostLoginV2 执行新的5步处理流程
func ProcessPostLoginV2(ctx *PostLoginProcessCtx) (*ProjectProcessResult, error) {
	result := &ProjectProcessResult{}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "开始登录后处理流程V2", "邮箱", ctx.Ctx.Email)

	// Step1: 获取项目列表，补充到12个，同步DB
	if err := PostLoginProcessStep1ProjectSetup(ctx); err != nil {
		return result, fmt.Errorf("流程1失败: %v", err)
	}
	result.TotalProjects = len(ctx.CliProjectList)

	// Step2: 开启API和生成token
	tokenResults, err := PostLoginProcessStep2TokenGeneration(ctx)
	if err != nil {
		return result, fmt.Errorf("流程2失败: %v", err)
	}
	result.CreateTokens = tokenResults

	// Step3: 检查billing状态
	if err := PostLoginProcessStep3BillingCheck(ctx); err != nil {
		return result, fmt.Errorf("流程3失败: %v", err)
	}

	// Step4: 绑定billing account
	boundResults, err := PostLoginProcessStep4BillingBind(ctx)
	if err != nil {
		return result, fmt.Errorf("流程4失败: %v", err)
	}
	result.BoundProjects = boundResults

	// Step5: 后置official_tokens同步
	syncResults, err := PostLoginProcessStep5TokenSync(ctx)
	if err != nil {
		return result, fmt.Errorf("流程5失败: %v", err)
	}
	result.SyncedTokens = syncResults

	result.Message = fmt.Sprintf("V2流程完成: 总计%d个项目, 新增%d个项目, 生成%d个Token, 绑卡%d个项目, 同步%d个官方Token",
		result.TotalProjects, result.CreatedProjects, result.CreateTokens, result.BoundProjects, result.SyncedTokens)

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "登录后处理流程V2完成", "结果", result.Message)
	return result, nil
}

// PostLoginProcessStep1ProjectSetup Step1: 获取cliProjectList列表，补充到12，1次性获取dbProjectsMp列表，同步DB
func PostLoginProcessStep1ProjectSetup(ctx *PostLoginProcessCtx) error {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Step1: 项目设置", "邮箱", ctx.Ctx.Email)

	// 1. 获取CLI项目列表（不用填扩展信息）
	cliProjects, err := getCLIProjects(ctx.Ctx)
	if err != nil {
		zlog.InfoWithCtx(ctx.Ctx.GinCtx, "获取到CLI项目失败", err.Error())
		return fmt.Errorf("获取CLI项目列表失败: %v", err)
	}

	// 转换为GCPProjectExt
	ctx.CliProjectList = make([]GCPProjectExt, len(cliProjects))
	for i, p := range cliProjects {
		ctx.CliProjectList[i] = GCPProjectExt{GCPProject: p}
	}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "获取到CLI项目", "数量", len(ctx.CliProjectList))

	// 2. 补充到12个项目
	targetCount := 12
	if len(ctx.CliProjectList) < targetCount {
		createdCount := targetCount - len(ctx.CliProjectList)
		createdProjects, err := createProjects(ctx.Ctx, createdCount)
		if err != nil {
			zlog.ErrorWithCtx(ctx.Ctx.GinCtx, "创建项目过程中有错误", err)
		}
		ctx.Result.CreatedProjects = createdCount
		ctx.Result.CreatedProjectsDetail = createdProjects

		// 添加创建的项目到列表
		for _, projectID := range createdProjects {
			ctx.CliProjectList = append(ctx.CliProjectList, GCPProjectExt{
				GCPProject: GCPProject{ProjectID: projectID, Name: "GATC Project"},
			})
		}

		zlog.InfoWithCtx(ctx.Ctx.GinCtx, "项目创建完成", "新增", len(createdProjects), "总计", len(ctx.CliProjectList))
	}
	ctx.Result.TotalProjects = len(ctx.CliProjectList)

	// 3. 1次性获取email下dbProjectsMp列表
	if err := loadDBProjects(ctx); err != nil {
		return fmt.Errorf("获取DB项目失败: %v", err)
	}

	// 4. 遍历cliProjectList，对不在db列表中的行进行insert db
	needDBReload := false
	for _, project := range ctx.CliProjectList {
		if ctx.DbProjectsMp[project.ProjectID] == nil {
			// 项目不在DB中，创建新记录
			account := &dao.GCPAccount{
				Email:         ctx.Ctx.Email,
				ProjectID:     project.ProjectID,
				BillingStatus: dao.BillingStatusUnbound,
				TokenStatus:   dao.TokenStatusNone,
				VMID:          ctx.Ctx.VMInstance.VMID,
				Sock5Proxy:    ctx.Ctx.VMInstance.Proxy,
				Region:        "us-central1",
				AuthStatus:    1,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}

			if err := dao.GGcpAccountDao.Create(ctx.Ctx.GinCtx, account); err != nil {
				zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("创建项目记录失败 项目ID:%s", project.ProjectID), err)
				continue
			}
			ctx.Result.SyncedProjects++
			ctx.Result.SyncedProjectsDetail = append(ctx.Result.SyncedProjectsDetail, project.ProjectID)

			zlog.InfoWithCtx(ctx.Ctx.GinCtx, "同步新项目到DB", "项目ID", project.ProjectID)
			needDBReload = true
		}
	}

	// 5. 若有插入db，则重新获取db列表
	if needDBReload {
		if err := loadDBProjects(ctx); err != nil {
			return fmt.Errorf("重新获取DB项目失败: %v", err)
		}
		zlog.InfoWithCtx(ctx.Ctx.GinCtx, "重新加载DB项目", "数量", len(ctx.DbProjectsMp))
	}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "Step1完成", "CLI项目", len(ctx.CliProjectList), "DB项目", len(ctx.DbProjectsMp))
	return nil
}

// PostLoginProcessStep2TokenGeneration Step2: 遍历cliProjectList，对db TokenStatus为None的项目执行开启4个服务，生成token
func PostLoginProcessStep2TokenGeneration(ctx *PostLoginProcessCtx) (int, error) {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Step2: Token生成", "邮箱", ctx.Ctx.Email)

	successCount := 0

	for _, project := range ctx.CliProjectList {
		dbProject := ctx.DbProjectsMp[project.ProjectID]
		if dbProject == nil || dbProject.TokenStatus != dao.TokenStatusNone {
			continue // 跳过不需要处理token的项目
		}

		// 开启4个服务，生成token
		success, token := generateTokenForProject(ctx.Ctx, project.ProjectID)
		if success {
			// TokenStatus变为GOT同时设置token字段
			dbProject.TokenStatus = dao.TokenStatusGot
			dbProject.OfficialToken = token
			dbProject.UpdatedAt = time.Now()
			successCount++
		} else {
			// 设置TokenStatusCreateFail
			dbProject.TokenStatus = dao.TokenStatusCreateFail
			dbProject.UpdatedAt = time.Now()
		}

		// 更新到dbProjectsMp，将更新写入db
		if err := dao.GGcpAccountDao.Save(ctx.Ctx.GinCtx, dbProject); err != nil {
			zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("更新项目Token状态失败 项目ID:%s", project.ProjectID), err)
		} else {
			zlog.InfoWithCtx(ctx.Ctx.GinCtx, "更新项目Token状态", "项目ID", project.ProjectID, "状态", dbProject.TokenStatus)
		}
	}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "Step2完成", "成功生成Token", successCount)
	return successCount, nil
}

// 仅开启新绑账单的项目
func PostLoginProcessV3Step4TokenGeneration(ctx *PostLoginProcessCtx) error {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Token生成 ", "邮箱", ctx.Ctx.Email)

	for _, project := range ctx.Result.BoundProjectsDetail {
		dbProject := ctx.DbProjectsMp[project]
		if dbProject == nil || dbProject.TokenStatus >= dao.TokenStatusGot {
			zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Token生成，项目跳过 ", "邮箱", ctx.Ctx.Email, "proj", project)
			continue // 跳过不需要处理token的项目
		}
		zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Token生成，项目开始 ", "邮箱", ctx.Ctx.Email, "proj", project)

		// 开启4个服务，生成token
		success, token := generateTokenForProject(ctx.Ctx, project)
		if success {
			// TokenStatus变为GOT同时设置token字段
			dbProject.TokenStatus = dao.TokenStatusGot
			dbProject.OfficialToken = token
			dbProject.UpdatedAt = time.Now()
			ctx.Result.CreateTokens++
		} else {
			// 设置TokenStatusCreateFail
			dbProject.TokenStatus = dao.TokenStatusCreateFail
			dbProject.UpdatedAt = time.Now()
		}

		// 更新到dbProjectsMp，将更新写入db
		if err := dao.GGcpAccountDao.Save(ctx.Ctx.GinCtx, dbProject); err != nil {
			zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("更新项目Token状态失败 项目ID:%s", project), err)
		} else {
			zlog.InfoWithCtx(ctx.Ctx.GinCtx, "更新项目Token状态", "项目ID", project, "状态", dbProject.TokenStatus)
		}
	}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "Step2完成", "成功生成Token", ctx.Result.CreateTokens)
	return nil
}

// PostLoginProcessStep3BillingCheck Step3: 检查billing状态并同步
func PostLoginProcessStep3BillingCheck(ctx *PostLoginProcessCtx) error {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Step3: Billing状态检查", "邮箱", ctx.Ctx.Email)

	// 3.1 cli获取所有绑账单的项目
	billingProjects, billingAccounts, err := getBillingProjectsInfo(ctx)
	if err != nil {
		return fmt.Errorf("获取billing信息失败: %v", err)
	}

	// 保存可用的billing账户
	ctx.BillingAccounts = billingAccounts

	// 3.2 若绑账单项目对应db状态是未绑定，更新dbProjectsMp中行的绑定状态
	for projectID, billingAccount := range billingProjects {
		dbProject := ctx.DbProjectsMp[projectID]
		if dbProject != nil && dbProject.BillingStatus == dao.BillingStatusUnbound {
			dbProject.BillingStatus = dao.BillingStatusBound
			dbProject.UpdatedAt = time.Now()

			// 将更新写入db
			if err := dao.GGcpAccountDao.Save(ctx.Ctx.GinCtx, dbProject); err != nil {
				zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("更新项目Billing状态失败 项目ID:%s", projectID), err)
			} else {
				zlog.InfoWithCtx(ctx.Ctx.GinCtx, "更新项目Billing状态", "项目ID", projectID, "账户", billingAccount)
			}
		}
	}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "Step3完成", "发现billing账户", len(ctx.BillingAccounts))
	return nil
}

// 辅助函数定义在文件末尾...
func getCLIProjects(workCtx *WorkCtx) ([]GCPProject, error) {
	cmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", workCtx.VMInstance.SSHUser, workCtx.VMInstance.ExternalIP),
		"gcloud projects list --format=json",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("获取CLI项目列表失败: %v, output: %s", err, string(output))
	}

	var projects []GCPProject
	if err := json.Unmarshal(output, &projects); err != nil {
		return nil, fmt.Errorf("解析项目JSON失败: str[%s] err: %v ", string(output), err)
	}

	return projects, nil
}

func loadDBProjects(ctx *PostLoginProcessCtx) error {
	projects, err := dao.GGcpAccountDao.GetProjectsByEmail(ctx.Ctx.GinCtx, ctx.Ctx.Email)
	if err != nil {
		return err
	}

	ctx.DbProjectsMp = make(map[string]*dao.GCPAccount)
	for i := range projects {
		ctx.DbProjectsMp[projects[i].ProjectID] = &projects[i]
	}

	return nil
}

func createProjects(workCtx *WorkCtx, count int) ([]string, error) {
	var createdProjects []string
	timestamp := time.Now().Unix()

	for i := 0; i < count; i++ {
		projectID := fmt.Sprintf("gatc-project-%d-%d", timestamp, rand.Intn(1000000))

		cmd := exec.Command(
			"ssh",
			"-i", constants.SSHKeyPath,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			fmt.Sprintf("%s@%s", workCtx.VMInstance.SSHUser, workCtx.VMInstance.ExternalIP),
			fmt.Sprintf("gcloud projects create %s --name='GATC Project'", projectID),
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			zlog.ErrorWithCtx(workCtx.GinCtx, fmt.Sprintf("创建项目失败 项目ID:%s output:%s", projectID, string(output)), err)
			break
		}

		createdProjects = append(createdProjects, projectID)
		zlog.InfoWithCtx(workCtx.GinCtx, "成功创建项目", "项目ID", projectID)
	}

	return createdProjects, nil
}

func generateTokenForProject(workCtx *WorkCtx, projectID string) (bool, string) {
	// 1. 启用必要的API服务
	services := []string{
		"cloudresourcemanager.googleapis.com",
		"serviceusage.googleapis.com",
		"apikeys.googleapis.com",
		"generativelanguage.googleapis.com",
	}

	for _, service := range services {
		cmd := exec.Command(
			"ssh",
			"-i", constants.SSHKeyPath,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			fmt.Sprintf("%s@%s", workCtx.VMInstance.SSHUser, workCtx.VMInstance.ExternalIP),
			fmt.Sprintf("gcloud services enable %s --project=%s", service, projectID),
		)

		if _, err := cmd.CombinedOutput(); err != nil {
			zlog.ErrorWithCtx(workCtx.GinCtx, fmt.Sprintf("启用服务失败 项目:%s 服务:%s", projectID, service), err)
			return false, ""
		}
	}

	// 2. 创建API Key
	cmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", workCtx.VMInstance.SSHUser, workCtx.VMInstance.ExternalIP),
		fmt.Sprintf(`gcloud services api-keys create --project="%s" --display-name="Gemini API Key" --api-target=service=generativelanguage.googleapis.com --format=json 2>/dev/null`, projectID),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		zlog.ErrorWithCtx(workCtx.GinCtx, fmt.Sprintf("创建API Key失败 项目:%s", projectID), err)
		return false, ""
	}

	// 3. 解析JSON响应获取keyString
	var response map[string]interface{}
	if err := json.Unmarshal(output, &response); err != nil {
		zlog.ErrorWithCtx(workCtx.GinCtx, "解析API Key响应失败", err)
		return false, ""
	}

	responseData, ok := response["response"].(map[string]interface{})
	if !ok {
		zlog.ErrorWithCtx(workCtx.GinCtx, "响应格式不正确，缺少response字段", nil)
		return false, ""
	}

	keyString, ok := responseData["keyString"].(string)
	if !ok || !strings.HasPrefix(keyString, "AIza") {
		zlog.ErrorWithCtx(workCtx.GinCtx, "响应格式不正确或token格式错误", nil)
		return false, ""
	}

	zlog.InfoWithCtx(workCtx.GinCtx, "成功生成Token", "项目ID", projectID, "token前缀", keyString[:10])
	return true, keyString
}

func getBillingProjectsInfo(ctx *PostLoginProcessCtx) (map[string]string, []string, error) {
	// 获取所有项目的billing信息
	// 返回: projectID -> billingAccount 映射, 所有billing账户列表
	projects := make(map[string]string)
	accounts := make([]string, 0)

	// 直接使用ctx中已经获取的项目列表，避免重复CLI调用
	// 遍历每个项目检查billing状态
	for _, project := range ctx.CliProjectList {
		projectID := project.ProjectID
		dbProject := ctx.DbProjectsMp[projectID]
		if dbProject != nil && dbProject.BillingStatus == dao.BillingStatusDetach {
			continue
		}

		cmdd := fmt.Sprintf("gcloud billing projects describe %s --format='value(billingAccountName)' 2>/dev/null || echo ''", projectID)

		// 检查项目是否绑了billing account
		billingCmd := exec.Command(
			"ssh",
			"-i", constants.SSHKeyPath,
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			fmt.Sprintf("%s@%s", ctx.Ctx.VMInstance.SSHUser, ctx.Ctx.VMInstance.ExternalIP),
			cmdd,
		)

		zlog.InfoWithCtx(ctx.Ctx.GinCtx, "[CMD]", cmdd)

		billingOutput, err := billingCmd.CombinedOutput()
		if err != nil {
			zlog.WarnWithCtx(ctx.Ctx.GinCtx, "检查项目billing状态失败", "项目ID", projectID, "错误", err)
			continue
		}

		billingAccount := strings.TrimSpace(string(billingOutput))
		if billingAccount != "" {
			projects[projectID] = billingAccount
			zlog.InfoWithCtx(ctx.Ctx.GinCtx, "发现绑定billing的项目", "项目ID", projectID, "billing账户", billingAccount)
		}
	}

	// 3. 获取所有可用的billing账户
	accountsCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.Ctx.VMInstance.SSHUser, ctx.Ctx.VMInstance.ExternalIP),
		"gcloud billing accounts list --filter='open=true' --format='value(name)' 2>/dev/null || echo ''",
	)

	accountsOutput, err := accountsCmd.CombinedOutput()
	if err != nil {
		zlog.WarnWithCtx(ctx.Ctx.GinCtx, "获取billing账户列表失败", "错误", err)
	} else {
		accountLines := strings.Split(strings.TrimSpace(string(accountsOutput)), "\n")
		for _, account := range accountLines {
			account = strings.TrimSpace(account)
			if account != "" {
				accounts = append(accounts, account)
			}
		}
	}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "获取billing信息完成", "绑定项目数", len(projects), "可用账户数", len(accounts))
	return projects, accounts, nil
}

func PostLoginProcessV3Step3BillingBind(ctx *PostLoginProcessCtx) error {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Step3: Billing绑定", "邮箱", ctx.Ctx.Email)
	if len(ctx.BillingAccounts) == 0 {
		zlog.InfoWithCtx(ctx.Ctx.GinCtx, "没有可用的billing账户，跳过绑定")
		return nil
	}
	// 使用第一个可用的billing账户
	billingAccount := ctx.BillingAccounts[0]
	successCount := 0
	// 对Unbound的项目，依次检查尝试绑定账单
	for _, project := range ctx.CliProjectList {
		dbProject := ctx.DbProjectsMp[project.ProjectID]
		if dbProject == nil || dbProject.BillingStatus != dao.BillingStatusUnbound {
			continue // 跳过已绑定或不存在的项目
		}
		// 尝试绑定账单
		if bindProjectToBilling(ctx.Ctx, project.ProjectID, billingAccount) {
			// 绑定OK的项目更新dbProjectsMp，写入db
			dbProject.BillingStatus = dao.BillingStatusBound
			dbProject.UpdatedAt = time.Now()

			if err := dao.GGcpAccountDao.Save(ctx.Ctx.GinCtx, dbProject); err != nil {
				zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("更新项目Billing状态失败 项目ID:%s", project.ProjectID), err)
			} else {
				ctx.Result.BoundProjects++
				ctx.Result.BoundProjectsDetail = append(ctx.Result.BoundProjectsDetail, project.ProjectID)
				zlog.InfoWithCtx(ctx.Ctx.GinCtx, "项目绑定成功", "项目ID", project.ProjectID, "账户", billingAccount)
			}
		}
		// 这里不设置绑定失败状态，按要求
	}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "Step4完成", "成功绑定", successCount)
	return nil
}

// PostLoginProcessStep4BillingBind Step4: 对Unbound的项目，依次尝试绑定账单
func PostLoginProcessStep4BillingBind(ctx *PostLoginProcessCtx) (int, error) {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Step4: Billing绑定", "邮箱", ctx.Ctx.Email)

	if len(ctx.BillingAccounts) == 0 {
		zlog.InfoWithCtx(ctx.Ctx.GinCtx, "没有可用的billing账户，跳过绑定")
		return 0, nil
	}

	// 使用第一个可用的billing账户
	billingAccount := ctx.BillingAccounts[0]
	successCount := 0

	// 对Unbound的项目，依次检查尝试绑定账单
	for _, project := range ctx.CliProjectList {
		dbProject := ctx.DbProjectsMp[project.ProjectID]
		if dbProject == nil || dbProject.BillingStatus != dao.BillingStatusUnbound {
			continue // 跳过已绑定或不存在的项目
		}

		// 尝试绑定账单
		if bindProjectToBilling(ctx.Ctx, project.ProjectID, billingAccount) {
			// 绑定OK的项目更新dbProjectsMp，写入db
			dbProject.BillingStatus = dao.BillingStatusBound
			dbProject.UpdatedAt = time.Now()

			if err := dao.GGcpAccountDao.Save(ctx.Ctx.GinCtx, dbProject); err != nil {
				zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("更新项目Billing状态失败 项目ID:%s", project.ProjectID), err)
			} else {
				successCount++
				zlog.InfoWithCtx(ctx.Ctx.GinCtx, "项目绑定成功", "项目ID", project.ProjectID, "账户", billingAccount)
			}
		}
		// 这里不设置绑定失败状态，按要求
	}

	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "Step4完成", "成功绑定", successCount)
	return successCount, nil
}

// PostLoginProcessStep5TokenSync Step5: 后置token数据同步，和前四步独立
func PostLoginProcessStep5TokenSync(ctx *PostLoginProcessCtx) (int, error) {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行Step5: Token同步", "邮箱", ctx.Ctx.Email)

	// 获取当前account表 billingStatus == BillingStatusBound && TokenStatus == TokenStatusGot 的记录作为集合1
	validProjects, err := getValidProjectsForTokenSync(ctx.Ctx.GinCtx, ctx.Ctx.Email)
	if err != nil {
		return 0, fmt.Errorf("获取有效项目失败: %v", err)
	}

	if len(validProjects) == 0 {
		zlog.InfoWithCtx(ctx.Ctx.GinCtx, "没有有效项目需要同步token")
		return 0, nil
	}

	// 获取所有official_tokens表email等于当前email的行的projectId，作为集合2
	existingTokens, err := getExistingOfficialTokens(ctx.Ctx.GinCtx, ctx.Ctx.Email)
	if err != nil {
		return 0, fmt.Errorf("获取existing tokens失败: %v", err)
	}

	// 在集合1不在集合2的记录（新获取的token），写入official_tokens表
	syncCount := 0
	for _, project := range validProjects {
		if !existingTokens[project.ProjectID] {
			// 新token，需要同步
			if err2 := insertOfficialToken(ctx.Ctx.GinCtx, ctx.Ctx.Email, project); err2 != nil {
				zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("插入official_token失败 项目ID:%s", project.ProjectID), err2)
				continue
			}
			syncCount++
			zlog.InfoWithCtx(ctx.Ctx.GinCtx, "同步token成功", "项目ID", project.ProjectID)
		}
	}

	ctx.Result.SyncedTokens = syncCount
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "Step5完成", "同步token数量", syncCount)
	return syncCount, nil
}

// 辅助函数

func bindProjectToBilling(workCtx *WorkCtx, projectID, billingAccount string) bool {
	cmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", workCtx.VMInstance.SSHUser, workCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud billing projects link %s --billing-account=%s", projectID, billingAccount),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// 符合预期，不记录详细错误
		zlog.InfoWithCtx(workCtx.GinCtx, fmt.Sprintf("绑定billing失败 项目:%s 账户:%s output:%s", projectID, billingAccount, string(output)), err)
		return false
	}

	return true
}

func getValidProjectsForTokenSync(ginCtx any, email string) ([]dao.GCPAccount, error) {
	allProjects, err := dao.GGcpAccountDao.GetProjectsByEmail(ginCtx.(*gin.Context), email)
	if err != nil {
		return nil, err
	}

	var validProjects []dao.GCPAccount
	for _, project := range allProjects {
		if project.BillingStatus == dao.BillingStatusBound &&
			project.TokenStatus == dao.TokenStatusGot &&
			project.OfficialToken != "" {
			validProjects = append(validProjects, project)
		}
	}

	return validProjects, nil
}

func getExistingOfficialTokens(ginCtx any, email string) (map[string]bool, error) {
	tokenDao := &dao.GormOfficialTokens{Email: email}
	tokens, err := tokenDao.GetList(ginCtx.(*gin.Context), "project_id", "", "", 0, 0)
	if err != nil {
		return nil, err
	}

	existingTokens := make(map[string]bool)
	for _, token := range tokens {
		if token.ProjectId != "" {
			existingTokens[token.ProjectId] = true
		}
	}

	return existingTokens, nil
}

func insertOfficialToken(ginCtx any, email string, project dao.GCPAccount) error {
	now := time.Now()

	officialToken := &dao.GormOfficialTokens{
		ChannelId: 16,                    // 固定16
		Name:      "gatc",                // 固定
		Token:     project.OfficialToken, // token
		Proxy:     project.Sock5Proxy,    // vm socket5地址
		Priority:  50,                    // priority
		Weight:    100,                   // weight
		RpmLimit:  0,                     // rpm_limit 0
		TpmLimit:  0,                     // tpm_limit 0
		Status:    1,                     // status
		TokenType: "static",              // token_type
		Email:     email,                 // email
		ProjectId: project.ProjectID,     // project_id
		CreatedAt: now,                   // created_at
		UpdatedAt: now,                   // updated_at
	}

	return officialToken.Create(ginCtx.(*gin.Context))
}

// ====================== V3 新增函数 ======================

func PostLoginProcessV3Step2BillingCheck(ctx *PostLoginProcessCtx) error {
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "执行V3 Step2: billing检查和解绑", "邮箱", ctx.Ctx.Email, "解绑模式", ctx.UnBindCurProj)

	// 3.1 cli获取所有绑账单的项目
	billingProjects, billingAccounts, err := getBillingProjectsInfo(ctx)
	if err != nil {
		return fmt.Errorf("获取billing信息失败: %v", err)
	}
	ctx.Result.OldBindingProjects = len(billingProjects)

	// 保存可用的billing账户
	ctx.BillingAccounts = billingAccounts

	if ctx.UnBindCurProj {
		for projectID, _ := range billingProjects {
			// do 解码
			if err = unbindProjectBilling(ctx.Ctx, projectID); err != nil {
				zlog.ErrorWithCtx(ctx.Ctx.GinCtx, "解绑项目billing失败", err)
				continue
			}
			ctx.Result.UnboundProjects++
			ctx.Result.UnboundProjectsDetail = append(ctx.Result.UnboundProjectsDetail, projectID)

			dbProject := ctx.DbProjectsMp[projectID]
			if dbProject.BillingStatus != dao.BillingStatusDetach {
				dbProject.BillingStatus = dao.BillingStatusDetach
				dbProject.UpdatedAt = time.Now()
				// 将更新写入db
				if err := dao.GGcpAccountDao.Save(ctx.Ctx.GinCtx, dbProject); err != nil {
					zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("更新项目Billing状态失败 项目ID:%s", projectID), err)
				} else {
					zlog.InfoWithCtx(ctx.Ctx.GinCtx, "更新项目Billing状态", "项目ID", projectID)
				}
			}
		}
	} else {
		for projectID, billingAccount := range billingProjects {
			dbProject := ctx.DbProjectsMp[projectID]
			if dbProject.BillingStatus == dao.BillingStatusUnbound {
				dbProject.BillingStatus = dao.BillingStatusBound
				dbProject.UpdatedAt = time.Now()
				// 将更新写入db
				if err := dao.GGcpAccountDao.Save(ctx.Ctx.GinCtx, dbProject); err != nil {
					zlog.ErrorWithCtx(ctx.Ctx.GinCtx, fmt.Sprintf("更新项目Billing状态失败 项目ID:%s", projectID), err)
				} else {
					zlog.InfoWithCtx(ctx.Ctx.GinCtx, "更新项目Billing状态", "项目ID", projectID, "账户", billingAccount)
				}
			}
		}
	}
	zlog.InfoWithCtx(ctx.Ctx.GinCtx, "V3 Step2完成", "发现billing账户", len(ctx.BillingAccounts))
	return nil
}

// 解绑项目billing的函数
func unbindProjectBilling(workCtx *WorkCtx, projectID string) error {
	cmd := exec.Command("ssh", "-i", constants.SSHKeyPath, "-o", "StrictHostKeyChecking=no",
		fmt.Sprintf("%s@%s", workCtx.VMInstance.SSHUser, workCtx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud billing projects unlink %s", projectID),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("解绑billing失败: %v, output: %s", err, string(output))
	}

	zlog.InfoWithCtx(workCtx.GinCtx, "解绑billing命令执行成功", "项目ID", projectID, "输出", string(output))
	return nil
}
