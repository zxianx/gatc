package gcloud

import (
	"fmt"
	"gatc/base/zlog"
	"gatc/dao"
)

// PostLoginProcessor 登录后处理器
type PostLoginProcessor struct {
	ctx            *WorkCtx
	projectManager *ProjectManager
}

// PostLoginProcessResult 登录后处理结果
type PostLoginProcessResult struct {
	SyncedProjects  int    `json:"synced_projects"`  // 同步的项目数
	CreatedProjects int    `json:"created_projects"` // 新创建的项目数
	BoundProjects   int    `json:"bound_projects"`   // 绑卡成功的项目数
	TokensCreated   int    `json:"tokens_created"`   // 创建的token数
	TotalProjects   int    `json:"total_projects"`   // 总项目数
	Message         string `json:"message"`          // 处理消息
}

func NewPostLoginProcessor(ctx *WorkCtx) *PostLoginProcessor {
	return &PostLoginProcessor{
		ctx:            ctx,
		projectManager: NewProjectManager(ctx),
	}
}

// ProcessPostLogin 执行登录后的完整处理流程
func (p *PostLoginProcessor) ProcessPostLogin(billingAccountID string) (*PostLoginProcessResult, error) {
	result := &PostLoginProcessResult{}

	zlog.InfoWithCtx(p.ctx.GinCtx, "开始登录后处理流程", "邮箱", p.ctx.Email)

	// 1. 遍历项目并同步到数据库
	projects, err := p.projectManager.ListProjects()
	if err != nil {
		return result, fmt.Errorf("获取项目列表失败: %v", err)
	}

	result.TotalProjects = len(projects)
	zlog.InfoWithCtx(p.ctx.GinCtx, "获取到项目", "数量", len(projects))

	// 同步项目到数据库
	if err := p.projectManager.SyncProjectsToDB(projects); err != nil {
		return result, fmt.Errorf("同步项目到数据库失败: %v", err)
	}
	result.SyncedProjects = len(projects)

	// 2. 尝试增加项目到12个
	targetCount := 12
	if len(projects) < targetCount {
		createdProjects, err := p.projectManager.CreateProjects(len(projects), targetCount)
		if err != nil {
			zlog.ErrorWithCtx(p.ctx.GinCtx, "创建项目过程中有错误", err)
		}
		result.CreatedProjects = len(createdProjects)
		result.TotalProjects += len(createdProjects)

		zlog.InfoWithCtx(p.ctx.GinCtx, "项目创建完成", "新增", len(createdProjects), "总计", result.TotalProjects)
	}

	// 3. 对未绑卡项目尝试绑卡（如果提供了billing account）
	if billingAccountID != "" {
		// 获取绑卡前的统计
		beforeBilling, _ := dao.GGcpAccountDao.GetUnboundProjectsByEmail(p.ctx.GinCtx, p.ctx.Email)
		beforeCount := len(beforeBilling)

		if err := p.projectManager.EnableBillingForProjects(billingAccountID); err != nil {
			zlog.ErrorWithCtx(p.ctx.GinCtx, "绑卡过程中有错误", err)
		}

		// 获取绑卡后的统计
		afterBilling, _ := dao.GGcpAccountDao.GetUnboundProjectsByEmail(p.ctx.GinCtx, p.ctx.Email)
		afterCount := len(afterBilling)

		result.BoundProjects = beforeCount - afterCount
		zlog.InfoWithCtx(p.ctx.GinCtx, "绑卡完成", "成功绑定", result.BoundProjects)

		// 4. 为新绑卡项目创建Gemini API Token
		if result.BoundProjects > 0 {
			beforeTokens, _ := dao.GGcpAccountDao.GetBoundProjectsWithoutToken(p.ctx.GinCtx, p.ctx.Email)
			beforeTokenCount := len(beforeTokens)

			if err := p.projectManager.CreateGeminiTokens(); err != nil {
				zlog.ErrorWithCtx(p.ctx.GinCtx, "创建Gemini Token过程中有错误", err)
			}

			afterTokens, _ := dao.GGcpAccountDao.GetBoundProjectsWithoutToken(p.ctx.GinCtx, p.ctx.Email)
			afterTokenCount := len(afterTokens)

			result.TokensCreated = beforeTokenCount - afterTokenCount
			zlog.InfoWithCtx(p.ctx.GinCtx, "Token创建完成", "成功创建", result.TokensCreated)
		}
	} else {
		zlog.InfoWithCtx(p.ctx.GinCtx, "未提供billing account，跳过绑卡流程")
	}

	// 生成结果消息
	if billingAccountID != "" {
		result.Message = fmt.Sprintf("处理完成: 同步%d个项目, 新增%d个项目, 绑卡%d个项目, 创建%d个Token",
			result.SyncedProjects, result.CreatedProjects, result.BoundProjects, result.TokensCreated)
	} else {
		result.Message = fmt.Sprintf("处理完成: 同步%d个项目, 新增%d个项目 (未进行绑卡)",
			result.SyncedProjects, result.CreatedProjects)
	}

	zlog.InfoWithCtx(p.ctx.GinCtx, "登录后处理流程完成", "结果", result.Message)
	return result, nil
}

// ProcessPostLoginV2 执行登录后的新5步处理流程
func (p *PostLoginProcessor) ProcessPostLoginV2() (*PostLoginProcessResult, error) {
	// 创建新的上下文
	ctx := &PostLoginProcessCtx{
		Ctx: p.ctx,
	}
	
	// 调用新的V2处理流程
	v2Result, err := ProcessPostLoginV2(ctx)
	if err != nil {
		return &PostLoginProcessResult{
			Message: fmt.Sprintf("V2流程执行失败: %v", err),
		}, err
	}

	// 转换结果格式以保持兼容性
	result := &PostLoginProcessResult{
		TotalProjects:   v2Result.TotalProjects,
		CreatedProjects: v2Result.CreatedProjects,
		TokensCreated:   v2Result.TokensCreated,
		BoundProjects:   v2Result.BoundProjects,
		Message:         v2Result.Message,
	}

	return result, nil
}

// GetProjectsSummary 获取项目汇总信息
func (p *PostLoginProcessor) GetProjectsSummary() (map[string]interface{}, error) {
	// 获取各种状态的项目数量
	allProjects, _, err := dao.GGcpAccountDao.List(p.ctx.GinCtx, 0, 0, 1000)
	if err != nil {
		return nil, fmt.Errorf("获取项目列表失败: %v", err)
	}

	summary := map[string]interface{}{
		"total_projects":      len(allProjects),
		"unbound_projects":    0,
		"bound_projects":      0,
		"projects_with_token": 0,
		"active_projects":     0,
	}

	for _, project := range allProjects {
		if project.Email != p.ctx.Email {
			continue
		}

		switch project.BillingStatus {
		case dao.BillingStatusUnbound:
			summary["unbound_projects"] = summary["unbound_projects"].(int) + 1
		case dao.BillingStatusBound:
			summary["bound_projects"] = summary["bound_projects"].(int) + 1
			if project.OfficialToken != "" {
				summary["projects_with_token"] = summary["projects_with_token"].(int) + 1
			}
		}

		if project.TokenStatus == dao.TokenStatusGot {
			summary["active_projects"] = summary["active_projects"].(int) + 1
		}
	}

	return summary, nil
}
