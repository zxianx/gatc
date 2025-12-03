package service

import (
	"fmt"
	"gatc/base/zlog"
	"gatc/constants"
	"gatc/dao"
	"gatc/service/gcloud"
	"github.com/gin-gonic/gin"
	"strings"
	"time"
)

type ProjectService struct{}

var GProjectService = &ProjectService{}

// ProcessProjectsV2 使用新的5步流程处理项目
func (s *ProjectService) ProcessProjectsV2(c *gin.Context, param *gcloud.ProjectProcessParam) (*gcloud.ProjectProcessResult, error) {
	// 创建WorkCtx - 从数据库获取账号状态
	accountStatus, err := dao.GGcpAccountDao.GetAccountStatus(c, param.Email)
	if err != nil {
		return &gcloud.ProjectProcessResult{
			Message: fmt.Sprintf("账号状态不存在，请先登录: %s", param.Email),
		}, err
	}

	// 检查登录状态
	if accountStatus.AuthStatus != dao.AuthStatusLoggedIn {
		return &gcloud.ProjectProcessResult{
			Message: "账号未登录，请先登录",
		}, fmt.Errorf("账号未登录")
	}

	// 获取VM实例信息
	vmInstance, err := dao.GVmInstanceDao.GetByVMID(c, accountStatus.VMID)
	if err != nil || vmInstance.Status != constants.VMStatusRunning {
		return &gcloud.ProjectProcessResult{
			Message: "VM不存在或状态异常，请检查VM状态",
		}, fmt.Errorf("VM不存在或状态异常")
	}

	// 创建WorkCtx
	ctx := &gcloud.WorkCtx{
		SessionID:  fmt.Sprintf("v2_process_%d_%s", time.Now().Unix(), strings.ReplaceAll(param.Email, "@", "_")),
		Email:      param.Email,
		VMInstance: vmInstance,
		GinCtx:     c,
	}

	// 创建PostLoginProcessor并执行V2流程
	postLoginProcessCtx := &gcloud.PostLoginProcessCtx{
		Ctx: ctx,
	}
	v2Result, err := gcloud.ProcessPostLoginV2(postLoginProcessCtx)
	if err != nil {
		return &gcloud.ProjectProcessResult{
			Message: fmt.Sprintf("V2流程执行失败: %v", err),
		}, err
	}

	return v2Result, err
}

// ProcessProjectsV2 使用新的5步流程处理项目
func (s *ProjectService) ProcessProjectsV3(c *gin.Context, param *gcloud.ProjectProcessParam) (*gcloud.ProjectProcessResult, error) {
	// 创建WorkCtx - 从数据库获取账号状态
	if param != nil {
		zlog.InfoWithCtx(c, "开始登录后处理流程ProcessProjectsV3", "邮箱", param.Email)
	}
	accountStatus, err := dao.GGcpAccountDao.GetAccountStatus(c, param.Email)
	if err != nil {
		return &gcloud.ProjectProcessResult{
			Message: fmt.Sprintf("账号状态不存在，请先登录: %s", param.Email),
		}, err
	}

	// 检查登录状态
	if accountStatus.AuthStatus != dao.AuthStatusLoggedIn {
		return &gcloud.ProjectProcessResult{
			Message: "账号未登录，请先登录",
		}, fmt.Errorf("账号未登录")
	}

	// 获取VM实例信息
	vmInstance, err := dao.GVmInstanceDao.GetByVMID(c, accountStatus.VMID)
	if err != nil || vmInstance.Status != constants.VMStatusRunning {
		return &gcloud.ProjectProcessResult{
			Message: "VM不存在或状态异常，请检查VM状态",
		}, fmt.Errorf("VM不存在或状态异常")
	}

	// 创建WorkCtx
	ctx := &gcloud.WorkCtx{
		SessionID:  fmt.Sprintf("v3_process_%d_%s", time.Now().Unix(), strings.ReplaceAll(param.Email, "@", "_")),
		Email:      param.Email,
		VMInstance: vmInstance,
		GinCtx:     c,
	}

	// 创建PostLoginProcessor并执行V3流程
	postLoginProcessCtx := &gcloud.PostLoginProcessCtx{
		Ctx:           ctx,
		UnBindCurProj: true,
	}
	if param.UnbindOldBillingProj != nil {
		postLoginProcessCtx.UnBindCurProj = *param.UnbindOldBillingProj
	}
	if err = gcloud.ProcessPostLoginV3(postLoginProcessCtx); err != nil {
		postLoginProcessCtx.Result.Message += fmt.Sprintf("V3流程执行失败: %v", err)
	}
	return &postLoginProcessCtx.Result, err
}

// SetTokenInvalidParam token失效请求参数
type SetTokenInvalidParam struct {
	ID        *int64 `json:"id" form:"id"`                 // 通过ID设置失效
	Email     string `json:"email" form:"email"`           // 通过email+projectId设置失效
	ProjectID string `json:"project_id" form:"project_id"` // 通过email+projectId设置失效
}

// SetTokenInvalid 设置token失效状态
func (s *ProjectService) SetTokenInvalid(c *gin.Context, param *SetTokenInvalidParam) error {
	// 验证参数：要么提供ID，要么提供email+projectId
	if param.ID == nil && (param.Email == "" || param.ProjectID == "") {
		return fmt.Errorf("必须提供id或者email+project_id")
	}

	// 通过ID设置失效
	if param.ID != nil {
		return dao.GGcpAccountDao.SetTokenInvalid(c, *param.ID)
	}

	// 通过email+projectId设置失效
	return dao.GGcpAccountDao.SetTokenInvalidByEmailAndProject(c, param.Email, param.ProjectID)
}

// GetEmailsWithUnboundProjects 获取包含未绑账单项目的邮箱列表
func (s *ProjectService) GetEmailsWithUnboundProjects(c *gin.Context) ([]string, error) {
	return dao.GGcpAccountDao.GetEmailsWithUnboundProjects(c)
}
