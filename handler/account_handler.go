package handler

import (
	"fmt"
	"gatc/base/ratelimit"
	"gatc/base/response"
	"gatc/base/zlog"
	"gatc/service"
	"gatc/service/gcloud"
	"gatc/tool"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// StartAccountRegistrationRequest 开始开号请求结构
type StartAccountRegistrationRequest struct {
	service.StartAccountRegistrationParam
}

// SubmitAuthKeyRequest 提交验证密钥请求结构
type SubmitAuthKeyRequest struct {
	service.SubmitAuthKeyParam
}

// ListAccountRequest 查询账户列表请求结构
type ListAccountRequest struct {
	service.ListAccountParam
}

// ProcessProjectsRequest 处理项目请求结构
type ProcessProjectsRequest struct {
	gcloud.ProjectProcessParam
	SkipRateLimit bool `json:"skip_rate_limit,omitempty"  form:"skip_rate_limit,omitempty"`
}

type AccountHandler struct {
	accountService *service.GcpAccountService
	projectService *service.ProjectService
	emailLimiter   *ratelimit.EmailRateLimiter // 邮箱请求频率限制器
}

func NewAccountHandler() *AccountHandler {
	return &AccountHandler{
		accountService: service.GGcpAccountService,
		projectService: service.GProjectService,
		emailLimiter:   ratelimit.NewEmailRateLimiter(10 * time.Minute), // 10分钟限制
	}
}

// StartRegistration 开始账户注册流程
func (h *AccountHandler) StartRegistration(c *gin.Context) {
	var req StartAccountRegistrationRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	result, err := h.accountService.StartAccountRegistration(c, &req.StartAccountRegistrationParam)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}

// SubmitAuthKey 提交验证密钥
func (h *AccountHandler) SubmitAuthKey(c *gin.Context) {
	var req SubmitAuthKeyRequest

	// 支持GET请求的查询参数绑定
	if c.Request.Method == "GET" {
		sessionID := c.Query("session_id")
		authKey := c.Query("auth_key")

		if sessionID == "" || authKey == "" {
			response.Error(c, http.StatusBadRequest, "Missing session_id or auth_key parameter")
			return
		}

		req.SessionID = sessionID
		req.AuthKey = authKey
	} else {
		// POST请求使用JSON绑定
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
			return
		}
	}

	result, err := h.accountService.SubmitAuthKey(c, &req.SubmitAuthKeyParam)
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to submit auth key", err)
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}

// ListAccounts 查询账户列表
func (h *AccountHandler) ListAccounts(c *gin.Context) {
	var req ListAccountRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	result, err := h.accountService.ListAccounts(c, &req.ListAccountParam)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}

// ProcessProjectsV2 处理项目流程V2（使用新的5步流程）
func (h *AccountHandler) ProcessProjectsV2(c *gin.Context) {
	var param ProcessProjectsRequest
	if err := c.ShouldBindQuery(&param); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	// 检查邮箱请求频率限制（V2也使用相同的限流器）
	if param.Email != "" {
		canProcess, remaining := h.emailLimiter.CanProcess(param.Email)
		if !canProcess {
			remainingMinutes := int(remaining.Minutes()) + 1 // 向上取整
			response.Error(c, http.StatusTooManyRequests,
				fmt.Sprintf("邮箱 %s 请求过于频繁，请等待 %d 分钟后再试", param.Email, remainingMinutes))
			return
		}
	}

	result, err := h.projectService.ProcessProjectsV2(c, &param.ProjectProcessParam)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}

func (h *AccountHandler) ProcessProjectsV3(c *gin.Context) {
	var param ProcessProjectsRequest
	if err := c.ShouldBindQuery(&param); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}
	_ = param.Adapt()
	// 检查邮箱请求频率限制
	if param.Email != "" && !param.SkipRateLimit {
		canProcess, remaining := h.emailLimiter.CanProcess(param.Email)
		if !canProcess {
			remainingSecond := int(remaining.Seconds()) // 向上取整
			response.Error(c, http.StatusTooManyRequests,
				fmt.Sprintf("邮箱 %s 请求过于频繁，请等待 %d s后再试", param.Email, remainingSecond))
			return
		}
	}

	result, err := h.projectService.ProcessProjectsV3(c, &param.ProjectProcessParam)
	if err != nil {
		zlog.ErrorWithCtx(c, "ProcessProjectsV3", err)
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}

// SetTokenInvalidRequest token设置失效请求结构
type SetTokenInvalidRequest struct {
	service.SetTokenInvalidParam
}

// SetTokenInvalid 设置token失效状态
func (h *AccountHandler) SetTokenInvalid(c *gin.Context) {
	var req SetTokenInvalidRequest

	// 支持GET和POST请求
	if c.Request.Method == "GET" {
		if err := c.ShouldBindQuery(&req); err != nil {
			response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
			return
		}
	} else {
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
			return
		}
	}

	err := h.projectService.SetTokenInvalid(c, &req.SetTokenInvalidParam)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, map[string]interface{}{
		"message": "Token已设置为失效状态",
	})
}

// GetEmailsWithUnboundProjects 获取包含未绑账单项目的邮箱列表
func (h *AccountHandler) GetEmailsWithUnboundProjects(c *gin.Context) {
	emails, err := h.projectService.GetEmailsWithUnboundProjects(c)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, map[string]interface{}{
		"emails": emails,
		"count":  len(emails),
	})
}

// DeleteAccountsRequest 删除账户请求结构
type DeleteAccountsRequest struct {
	service.DeleteAccountParam
}

// DeleteAccounts 删除账户（支持单个或批量）
func (h *AccountHandler) DeleteAccounts(c *gin.Context) {
	var req DeleteAccountsRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	// 验证参数
	if req.Email == "" && len(req.Emails) == 0 {
		response.Error(c, http.StatusBadRequest, "邮箱不能为空，请提供 email 或 emails 参数")
		return
	}

	result, err := h.accountService.DeleteAccounts(c, &req.DeleteAccountParam)
	if err != nil {
		zlog.ErrorWithMsgAndCtx(c, "删除账户失败", "error", err)
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, map[string]any{
		"message":       "删除成功",
		"deleted_count": result.DeletedCount,
		"emails":        result.Emails,
	})
}

// CreateProjBillingUnbindV4Request V4创建项目并解绑账单请求
type CreateProjBillingUnbindV4Request struct {
	gcloud.ProjectProcessV4Param
}

// CreateProjBillingUnbindV4 创建项目并解绑账单（V4）
func (h *AccountHandler) CreateProjBillingUnbindV4(c *gin.Context) {
	var req CreateProjBillingUnbindV4Request
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	result, err := gcloud.CreateProjBillingUnbindV4(c, &req.ProjectProcessV4Param)
	if err != nil {
		zlog.ErrorWithMsgAndCtx(c, "CreateProjBillingUnbindV4 V4创建项目并解绑账单失败", "error", err)
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}

// KeyWithdrawKeySaveV4Request V4提取Key并保存请求
type KeyWithdrawKeySaveV4Request struct {
	gcloud.KeyWithdrawV4Param
}

// KeyWithdrawKeySaveV4 提取Key并保存（V4）
func (h *AccountHandler) KeyWithdrawKeySaveV4(c *gin.Context) {
	var req KeyWithdrawKeySaveV4Request
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	var result *gcloud.KeyWithdrawKeySaveV4Result
	var err error

	// defer 检查：如果有失败或实际提取数小于待提项目数，异步触发同步
	defer func() {
		if result == nil {
			return
		}
		// 条件：有失败项目，或者成功数小于处理数（意味着有失败）
		if result.FailedTokens > 0 || result.SuccessTokens < result.ProcessedProjects {
			zlog.InfoWithCtx(c, "KeyWithdrawKeySaveV4 触发异步同步",
				"email", req.KeyWithdrawV4Param.Email,
				"processed", result.ProcessedProjects,
				"success", result.SuccessTokens,
				"failed", result.FailedTokens)

			// 异步执行同步，使用特殊参数调用 CreateProjBillingUnbindV4
			go func(email string) {
				// 创建一个新的 context 用于异步操作（复制必要信息）
				syncParam := &gcloud.ProjectProcessV4Param{
					Email:                email,
					MaxCreateProjNum:     0,                  // 不创建新项目
					UnbindOldBillingProj: tool.NewPtr(false), // 不解绑账单
				}
				// 使用 background context 避免依赖已完成的请求 context
				syncResult, err := gcloud.CreateProjBillingUnbindV4(c, syncParam)
				if err != nil {
					zlog.ErrorWithMsgAndCtx(c, "KeyWithdrawKeySaveV4 异步同步失败", err, "email", email)
				} else {
					zlog.InfoWithCtx(c, "KeyWithdrawKeySaveV4 异步同步完成",
						"email", email,
						"result", syncResult.Message)
				}
			}(req.Email)
		}
	}()

	result, err = gcloud.KeyWithdrawKeySaveV4(c, &req.KeyWithdrawV4Param)
	if err != nil {
		zlog.ErrorWithMsgAndCtx(c, "KeyWithdrawKeySaveV4 V4提取Key并保存失败", "error", err)
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}
