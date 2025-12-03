package handler

import (
	"fmt"
	"gatc/base/ratelimit"
	"gatc/base/response"
	"gatc/service"
	"gatc/service/gcloud"
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
