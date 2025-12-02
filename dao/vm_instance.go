package dao

import (
	"gatc/base/zlog"
	"gatc/helpers"
	"time"

	"github.com/gin-gonic/gin"
)

// VMInstance VM实例数据库模型
type VMInstance struct {
	ID            int64     `json:"id" gorm:"primarykey;autoIncrement"`
	VMID          string    `json:"vm_id" gorm:"column:vm_id;size:128;uniqueIndex;not null"`
	VMName        string    `json:"vm_name" gorm:"column:vm_name;size:128;not null"`
	Zone          string    `json:"zone" gorm:"column:zone;size:64;not null"`
	MachineType   string    `json:"machine_type" gorm:"column:machine_type;size:64;not null"`
	ExternalIP    string    `json:"external_ip" gorm:"column:external_ip;size:45"`
	InternalIP    string    `json:"internal_ip" gorm:"column:internal_ip;size:45"`
	Proxy         string    `json:"proxy" gorm:"column:proxy;size:255"` // 代理信息，格式：user1:pass123@ip:port
	ProxyType     string    `json:"proxy_type" gorm:"column:proxy_type;size:16"` // 代理类型：socks5/tinyproxy
	SSHUser       string    `json:"ssh_user" gorm:"column:ssh_user;size:64"`
	SSHKeyContent string    `json:"ssh_key_content" gorm:"column:ssh_key_content;type:text"`
	Status        int       `json:"status" gorm:"column:status;not null;default:1;index"`
	CreatedAt     time.Time `json:"created_at" gorm:"column:created_at;index"`
	UpdatedAt     time.Time `json:"updated_at" gorm:"column:updated_at;index"`
}

func (VMInstance) TableName() string {
	return "vm_instances"
}

// VMInstanceDao VM实例数据访问对象
type VMInstanceDao struct{}

var GVmInstanceDao = &VMInstanceDao{}

// Create 创建VM实例
func (d *VMInstanceDao) Create(c *gin.Context, vm *VMInstance) error {
	zlog.InfoWithCtx(c, "Creating VM instance in database", "vmId", vm.VMID)
	err := helpers.GatcDbClient.Create(vm).Error
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to create VM instance", err)
	} else {
		zlog.InfoWithCtx(c, "VM instance created successfully", "vmId", vm.VMID, "id", vm.ID)
	}
	return err
}

// GetByVMID 根据VM ID查询
func (d *VMInstanceDao) GetByVMID(c *gin.Context, vmID string) (*VMInstance, error) {
	zlog.InfoWithCtx(c, "Querying VM instance by ID", "vmId", vmID)
	var vm VMInstance
	err := helpers.GatcDbClient.Where("vm_id = ?", vmID).First(&vm).Error
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to query VM instance", err)
		return nil, err
	}
	zlog.InfoWithCtx(c, "VM instance found", "vmId", vmID, "status", vm.Status)
	return &vm, nil
}

// UpdateStatus 更新状态
func (d *VMInstanceDao) UpdateStatus(c *gin.Context, vmID string, status int) error {
	zlog.InfoWithCtx(c, "Updating VM status", "vmId", vmID, "newStatus", status)
	err := helpers.GatcDbClient.Model(&VMInstance{}).
		Where("vm_id = ?", vmID).
		Update("status", status).Error
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to update VM status", err)
	} else {
		zlog.InfoWithCtx(c, "VM status updated successfully", "vmId", vmID, "status", status)
	}
	return err
}

// Save 保存VM实例
func (d *VMInstanceDao) Save(c *gin.Context, vm *VMInstance) error {
	zlog.InfoWithCtx(c, "Saving VM instance", "vmId", vm.VMID)
	err := helpers.GatcDbClient.Save(vm).Error
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to save VM instance", err)
	}
	return err
}

// List 分页查询VM列表
func (d *VMInstanceDao) List(c *gin.Context, status int, offset, limit int) ([]VMInstance, int64, error) {
	zlog.InfoWithCtx(c, "Querying VM list", "status", status, "offset", offset, "limit", limit)

	query := helpers.GatcDbClient.Model(&VMInstance{})

	if status > 0 {
		query = query.Where("status = ?", status)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		zlog.ErrorWithCtx(c, "Failed to count VMs", err)
		return nil, 0, err
	}

	var items []VMInstance
	err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&items).Error
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to query VM list", err)
		return nil, 0, err
	}

	zlog.InfoWithCtx(c, "VM list queried successfully", "total", total, "returned", len(items))
	return items, total, nil
}

// Delete 软删除VM实例
func (d *VMInstanceDao) Delete(c *gin.Context, vmID string) error {
	zlog.InfoWithCtx(c, "Soft deleting VM instance", "vmId", vmID)
	err := helpers.GatcDbClient.Model(&VMInstance{}).
		Where("vm_id = ?", vmID).
		Update("status", 3).Error // 3 = deleted
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to soft delete VM", err)
	} else {
		zlog.InfoWithCtx(c, "VM soft deleted successfully", "vmId", vmID)
	}
	return err
}

// GetVMsCreatedBefore 获取指定时间之前创建的VM列表
func (d *VMInstanceDao) GetVMsCreatedBefore(c *gin.Context, cutoffTime time.Time) ([]VMInstance, error) {
	zlog.InfoWithCtx(c, "Querying VMs created before", "cutoffTime", cutoffTime)
	
	var vms []VMInstance
	err := helpers.GatcDbClient.Where("created_at < ? AND status != ?", cutoffTime, 3).Find(&vms).Error
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to query old VMs", err)
		return nil, err
	}
	
	zlog.InfoWithCtx(c, "Found VMs created before cutoff", "count", len(vms), "cutoffTime", cutoffTime)
	return vms, nil
}

// GetActiveVMs 获取所有非已删除状态的VM实例
func (d *VMInstanceDao) GetActiveVMs(c *gin.Context) ([]VMInstance, error) {
	zlog.InfoWithCtx(c, "Querying all active VMs")
	
	var vms []VMInstance
	err := helpers.GatcDbClient.Where("status != ?", 3).Find(&vms).Error
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to query active VMs", err)
		return nil, err
	}
	
	zlog.InfoWithCtx(c, "Found active VMs", "count", len(vms))
	return vms, nil
}

// BatchUpdateStatusDeleted 批量设置VM状态为已删除
func (d *VMInstanceDao) BatchUpdateStatusDeleted(c *gin.Context, vmIDs []string) error {
	zlog.InfoWithCtx(c, "Batch updating VM status to deleted", "count", len(vmIDs))
	
	if len(vmIDs) == 0 {
		return nil
	}
	
	err := helpers.GatcDbClient.Model(&VMInstance{}).
		Where("vm_id IN ?", vmIDs).
		Update("status", 3).Error
	
	if err != nil {
		zlog.ErrorWithCtx(c, "Failed to batch update VM status", err)
	} else {
		zlog.InfoWithCtx(c, "Successfully batch updated VM status", "count", len(vmIDs))
	}
	
	return err
}
