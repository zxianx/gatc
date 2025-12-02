package gcloud

import (
	"fmt"
	"gatc/base/zlog"
	"gatc/constants"
	"gatc/dao"
	"os/exec"
	"strings"
	"time"
)

// GCPProject GCP项目结构
// GCPProject 已在 post_login_process_v2.go 中定义

// ProjectManager 项目管理器
type ProjectManager struct {
	ctx *WorkCtx
}

func NewProjectManager(ctx *WorkCtx) *ProjectManager {
	return &ProjectManager{
		ctx: ctx,
	}
}

// SyncProjectsToDB 同步项目到数据库
func (pm *ProjectManager) SyncProjectsToDB(projects []GCPProject) error {
	for _, project := range projects {
		// 检查项目是否已存在
		existingAccount, err := dao.GGcpAccountDao.GetByEmailAndProject(pm.ctx.GinCtx, pm.ctx.Email, project.ProjectID)
		if err != nil && err.Error() != "record not found" {
			zlog.ErrorWithCtx(pm.ctx.GinCtx, "查询项目失败", err)
			continue
		}

		if existingAccount == nil {
			// 项目不存在，创建新记录
			account := &dao.GCPAccount{
				Email:         pm.ctx.Email,
				ProjectID:     project.ProjectID,
				BillingStatus: dao.BillingStatusUnbound, // 默认未绑卡
				TokenStatus:   dao.TokenStatusNone,      // 默认无token
				VMID:          pm.ctx.VMInstance.VMID,
				Region:        "us-central1", // 默认区域
				AuthStatus:    1,             // 认证成功状态
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}

			if err := dao.GGcpAccountDao.Create(pm.ctx.GinCtx, account); err != nil {
				zlog.ErrorWithCtx(pm.ctx.GinCtx, "创建项目记录失败", err)
				return fmt.Errorf("failed to create project record: %v", err)
			}

			zlog.InfoWithCtx(pm.ctx.GinCtx, "同步项目到数据库", "项目ID", project.ProjectID)
		} else {
			zlog.InfoWithCtx(pm.ctx.GinCtx, "项目已存在，跳过", "项目ID", project.ProjectID)
		}
	}

	return nil
}

// CreateProjects 创建新项目，尽量补全到12个
func (pm *ProjectManager) CreateProjects(currentCount int, targetCount int) ([]string, error) {
	if targetCount > 12 {
		targetCount = 12
	}

	needCount := targetCount - currentCount
	if needCount <= 0 {
		zlog.InfoWithCtx(pm.ctx.GinCtx, "项目数量已足够", "当前数量", currentCount, "目标数量", targetCount)
		return []string{}, nil
	}

	zlog.InfoWithCtx(pm.ctx.GinCtx, "开始创建新项目", "需要创建", needCount)

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
			fmt.Sprintf("%s@%s", pm.ctx.VMInstance.SSHUser, pm.ctx.VMInstance.ExternalIP),
			fmt.Sprintf("gcloud projects create %s --name='GATC Project %d'", projectID, i+1),
		)

		output, err := cmd.Output()
		if err != nil {
			zlog.ErrorWithCtx(pm.ctx.GinCtx, "创建项目失败", fmt.Errorf("项目: %s, 错误: %v, 输出: %s", projectID, err, string(output)))
			continue
		}

		zlog.InfoWithCtx(pm.ctx.GinCtx, "项目创建成功", "项目ID", projectID)
		createdProjects = append(createdProjects, projectID)

		// 创建对应的数据库记录
		account := &dao.GCPAccount{
			Email:         pm.ctx.Email,
			ProjectID:     projectID,
			BillingStatus: dao.BillingStatusUnbound, // 新项目默认未绑卡
			TokenStatus:   dao.TokenStatusNone,      // 新项目默认无token
			VMID:          pm.ctx.VMInstance.VMID,
			Region:        "us-central1",
			AuthStatus:    1, // 认证成功状态
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}

		if err := dao.GGcpAccountDao.Create(pm.ctx.GinCtx, account); err != nil {
			zlog.ErrorWithCtx(pm.ctx.GinCtx, "创建项目数据库记录失败", err)
		}

		// 短暂等待，避免创建过快
		time.Sleep(2 * time.Second)
	}

	zlog.InfoWithCtx(pm.ctx.GinCtx, "项目创建完成", "成功创建", len(createdProjects))
	return createdProjects, nil
}

// EnableBillingForProjects 为未绑卡项目尝试绑卡
func (pm *ProjectManager) EnableBillingForProjects(billingAccountID string) error {
	// 获取该邮箱下所有未绑卡的项目
	unboundProjects, err := dao.GGcpAccountDao.GetUnboundProjectsByEmail(pm.ctx.GinCtx, pm.ctx.Email)
	if err != nil {
		return fmt.Errorf("failed to get unbound projects: %v", err)
	}

	zlog.InfoWithCtx(pm.ctx.GinCtx, "开始为项目绑卡", "未绑卡项目数", len(unboundProjects))

	for _, project := range unboundProjects {
		if err := pm.enableBillingForProject(project.ProjectID, billingAccountID); err != nil {
			zlog.ErrorWithCtx(pm.ctx.GinCtx, "项目绑卡失败", fmt.Errorf("项目: %s, 错误: %v", project.ProjectID, err))
			continue
		}

		// 更新数据库状态
		project.BillingStatus = dao.BillingStatusBound
		// 绑卡不影响token状态，移除此设置
		project.UpdatedAt = time.Now()

		if err := dao.GGcpAccountDao.Save(pm.ctx.GinCtx, &project); err != nil {
			zlog.ErrorWithCtx(pm.ctx.GinCtx, "更新项目绑卡状态失败", err)
		}

		zlog.InfoWithCtx(pm.ctx.GinCtx, "项目绑卡成功", "项目ID", project.ProjectID)
	}

	return nil
}

// enableBillingForProject 为单个项目启用计费
func (pm *ProjectManager) enableBillingForProject(projectID, billingAccountID string) error {
	cmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", pm.ctx.VMInstance.SSHUser, pm.ctx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud beta billing projects link %s --billing-account=%s", projectID, billingAccountID),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to enable billing: %v, output: %s", err, string(output))
	}

	return nil
}

// CreateGeminiTokens 为新绑卡项目创建Gemini API Token
func (pm *ProjectManager) CreateGeminiTokens() error {
	// 获取新绑卡但还没有token的项目
	projects, err := dao.GGcpAccountDao.GetBoundProjectsWithoutToken(pm.ctx.GinCtx, pm.ctx.Email)
	if err != nil {
		return fmt.Errorf("failed to get projects without token: %v", err)
	}

	zlog.InfoWithCtx(pm.ctx.GinCtx, "开始为项目创建Gemini Token", "项目数", len(projects))

	for _, project := range projects {
		token, err := pm.createGeminiTokenForProject(project.ProjectID)
		if err != nil {
			zlog.ErrorWithCtx(pm.ctx.GinCtx, "创建Gemini Token失败", fmt.Errorf("项目: %s, 错误: %v", project.ProjectID, err))
			continue
		}

		// 更新数据库中的token
		project.OfficialToken = token
		project.UpdatedAt = time.Now()

		if err := dao.GGcpAccountDao.Save(pm.ctx.GinCtx, &project); err != nil {
			zlog.ErrorWithCtx(pm.ctx.GinCtx, "保存Gemini Token失败", err)
			continue
		}

		zlog.InfoWithCtx(pm.ctx.GinCtx, "Gemini Token创建成功", "项目ID", project.ProjectID)
	}

	return nil
}

// createGeminiTokenForProject 为单个项目创建Gemini API Token
func (pm *ProjectManager) createGeminiTokenForProject(projectID string) (string, error) {
	// 1. 启用AI Platform API
	enableCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", pm.ctx.VMInstance.SSHUser, pm.ctx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud services enable aiplatform.googleapis.com --project=%s", projectID),
	)

	if output, err := enableCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to enable AI platform API: %v, output: %s", err, string(output))
	}

	// 2. 创建服务账户
	serviceAccountName := "gemini-api-sa"
	createSACmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", pm.ctx.VMInstance.SSHUser, pm.ctx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud iam service-accounts create %s --display-name='Gemini API Service Account' --project=%s", serviceAccountName, projectID),
	)

	if output, err := createSACmd.CombinedOutput(); err != nil {
		// 如果服务账户已存在，忽略错误
		if !strings.Contains(string(output), "already exists") {
			return "", fmt.Errorf("failed to create service account: %v, output: %s", err, string(output))
		}
	}

	// 3. 创建并下载密钥
	serviceAccountEmail := fmt.Sprintf("%s@%s.iam.gserviceaccount.com", serviceAccountName, projectID)
	keyCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", pm.ctx.VMInstance.SSHUser, pm.ctx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud iam service-accounts keys create /tmp/%s-key.json --iam-account=%s --project=%s", projectID, serviceAccountEmail, projectID),
	)

	if output, err := keyCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to create service account key: %v, output: %s", err, string(output))
	}

	// 4. 读取生成的密钥文件
	catCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", pm.ctx.VMInstance.SSHUser, pm.ctx.VMInstance.ExternalIP),
		fmt.Sprintf("cat /tmp/%s-key.json", projectID),
	)

	keyContent, err := catCmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to read service account key: %v", err)
	}

	// 5. 清理临时文件
	rmCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", pm.ctx.VMInstance.SSHUser, pm.ctx.VMInstance.ExternalIP),
		fmt.Sprintf("rm /tmp/%s-key.json", projectID),
	)
	rmCmd.CombinedOutput() // 忽略清理错误

	return strings.TrimSpace(string(keyContent)), nil
}
