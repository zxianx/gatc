package handler

import (
	"gatc/base/response"
	"gatc/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CreateVMRequest 创建VM请求结构
type CreateVMRequest struct {
	service.BatchCreateVMParam
}

// DeleteVMRequest 删除VM请求结构
type DeleteVMRequest struct {
	service.DeleteVMParam
	service.BatchDeleteVMParam
}

// ListVMRequest 查询VM列表请求结构
type ListVMRequest struct {
	service.ListVMParam
}

// GetVMRequest 查询单个VM请求结构
type GetVMRequest struct {
	service.GetVMParam
}

// RefreshVMIPRequest 刷新VM外网IP请求结构
type RefreshVMIPRequest struct {
	service.RefreshVMIPParam
}

type VMHandler struct {
	vmService *service.VMService
}

func NewVMHandler() *VMHandler {
	return &VMHandler{
		vmService: service.GVmService,
	}
}

func (h *VMHandler) CreateVM(c *gin.Context) {
	var req CreateVMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	if req.Num > 1 {
		batchParam := &req.BatchCreateVMParam
		result, err := h.vmService.BatchCreateVM(c, batchParam)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(c, result)
	} else {
		createParam := &service.CreateVMParam{
			Zone:        req.Zone,
			MachineType: req.MachineType,
			Tag:         req.Tag,
			ProxyType:   req.ProxyType,
		}
		result, err := h.vmService.CreateVM(c, createParam)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(c, result)
	}
}

func (h *VMHandler) DeleteVM(c *gin.Context) {
	var req DeleteVMRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	if req.DeleteVMParam.VMID != "" {
		deleteParam := &req.DeleteVMParam
		result, err := h.vmService.DeleteVM(c, deleteParam)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(c, result)
	} else {
		batchParam := &req.BatchDeleteVMParam
		result, err := h.vmService.BatchDeleteVM(c, batchParam)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
		response.Success(c, result)
	}
}

func (h *VMHandler) ListVMs(c *gin.Context) {
	var req ListVMRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	result, err := h.vmService.ListVMs(c, &req.ListVMParam)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}

func (h *VMHandler) GetVM(c *gin.Context) {
	var req GetVMRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	result, err := h.vmService.GetVM(c, &req.GetVMParam)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}

func (h *VMHandler) RefreshVMIP(c *gin.Context) {
	var req RefreshVMIPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	result, err := h.vmService.RefreshVMIP(c, &req.RefreshVMIPParam)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	response.Success(c, result)
}
