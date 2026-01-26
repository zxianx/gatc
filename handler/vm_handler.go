package handler

import (
	"fmt"
	"gatc/base/response"
	"gatc/base/zlog"
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

// ReplaceProxyResourceRequest 替换代理资源请求结构
type ReplaceProxyResourceRequest struct {
	service.ReplaceProxyResourceParam
}
type ReplaceProxyResourceRes struct {
	ReplaceRes service.ReplaceProxyResourceV2Result `json:"replace_res"`
	SyncRes    service.SyncProxyPoolFromVMsRes      `json:"sync_res"`
	Err        string                               `json:"err"`
}

// ReplaceProxyResource 替换代理资源接口
func (h *VMHandler) ReplaceProxyResource(c *gin.Context) {

	/*
		参数同 BatchCreateVMParam （含num）， 类型固定得是 “server或httpProxyServer”（两者是同一种） ， 否则错误
		1、 创建一批新vm（代理机），代理地址格式类似 “http://35.208.147.190:1081/px”
		2、 查询 proxy_pool 表， 按 created_at  倒序 limit  num 个， 称为lastBatchProxy
		3、 新的vm的代理插入 proxy_pool 表
		proxy_pool 表proxy_type = server, proxy =  “http://35.208.147.190:1081” (没有"/px" 后缀)
		4、 将第lastBatchProxy的 n 个, n <= num,  从新建代理选前n个， 建立1:1映射 toReplaceMap
		5、 for toReplaceMap ,  official_tokens 表 base_url 字段，  按 like "http://旧代理地址/px%"  查询，替换其中的代理到新代理。
		6、 lastBatchProxy 的 status 置0
		7、 lastBatchProxy 获取proxy 列表 ， 加 /px 后缀，  逐个查询 vm_instance 表。 获取proxy = 这个 proxu 的行的 vm_id. 作为 to_del_vm
		8、 to_del_vm 走删除vm接口删除
	*/

	var req ReplaceProxyResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	result, err := h.vmService.ReplaceProxyResource(c, &req.ReplaceProxyResourceParam)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	response.Success(c, result)
}

// ReplaceProxyResourceV2 替换代理资源接口V2版本
func (h *VMHandler) ReplaceProxyResourceV2(c *gin.Context) {
	var req ReplaceProxyResourceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "Invalid request parameters: "+err.Error())
		return
	}

	var result ReplaceProxyResourceRes

	replaceRes, err := h.vmService.ReplaceProxyResourceV2(c, &req.ReplaceProxyResourceParam)
	// 无论成败都记录结果
	replaceRes.Message = fmt.Sprintf("代理资源替换V2完成: 标记预删除VM %d个, 新建VM %d个",
		replaceRes.MarkedPendingDelete, replaceRes.NewVMsCreated)
	result.ReplaceRes = replaceRes

	// 如果替换失败，直接返回错误
	if err != nil {
		result.Err += "\t" + err.Error()
		response.Success(c, result)
		return
	}

	zlog.InfoWithCtx(c, "ReplaceProxyResourceV2 Step 3: Syncing proxy pool from VMs")
	syncRes, err2 := service.GVmService.SyncProxyPoolFromVMs(c)
	if err2 != nil {
		result.Err += "\t" + err2.Error()
		zlog.ErrorWithCtx(c, "Failed to sync proxy pool from VMs", err2)
	} else {
		zlog.InfoWithCtx(c, "ReplaceProxyResourceV2 Proxy pool synced successfully")
	}
	result.SyncRes = syncRes
	response.Success(c, result)
}
