package dao

import (
	"gatc/helpers"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	// 绑卡状态
	BillingStatusUnbound = 0
	BillingStatusBound   = 1
	BillingStatusDetach  = 2 // 绑定过，已解除

	// Token Status（token状态）
	TokenStatusNone       = 0 // 无token
	TokenStatusCreateFail = 1 // token创建失败
	TokenStatusGot        = 2 // 已获取token
	TokenStatusInvalid    = 3 // token失效（外部设置）

	// AuthStatus - 账号认证状态
	AuthStatusNotLogin    = 0 // 未登录
	AuthStatusLoggedIn    = 1 // 已登录
	AuthStatusLoginFailed = 2 // 登录失败
	AuthStatusVMError     = 3 // VM异常
)

// GCPAccount GCP账户数据库模型
type GCPAccount struct {
	ID              int64     `json:"id" gorm:"primarykey;autoIncrement"`
	Email           string    `json:"email" gorm:"column:email;size:255;not null;index;uniqueIndex:idx_email_project"`
	ProjectID       string    `json:"project_id" gorm:"column:project_id;size:128;not null;default:'';uniqueIndex:idx_email_project"`
	BillingStatus   int       `json:"billing_status" gorm:"column:billing_status;not null;default:0;index"`
	TokenStatus     int       `json:"token_status" gorm:"column:token_status;not null;default:0;index"`
	ProjectStatus   int       `json:"project_status" gorm:"column:project_status;not null;default:0;index"` // 字段还没用到
	VMID            string    `json:"vm_id" gorm:"column:vm_id;size:128;not null;default:'';index"`
	Sock5Proxy      string    `json:"sock5_proxy" gorm:"column:sock5_proxy;not null;default:'';size:128"` // VM里面的信息，额外存个字段, 沿用字段名，实际多种类型proxy
	OfficialToken   string    `json:"official_token" gorm:"column:official_token;not null;type:text"`
	OfficialTokenId int64     `json:"official_token_id" gorm:"column:official_token_id;not null;index"` //OfficialToken 写其他表，暂时没id， 插入成功这里标记个1
	Region          string    `json:"region" gorm:"column:region;size:64"`
	AuthDebugInfo   string    `json:"auth_debug_info" gorm:"column:auth_debug_info;type:text"`
	AuthStatus      int       `json:"auth_status" gorm:"column:auth_status;not null;default:0"`
	CreatedAt       time.Time `json:"created_at" gorm:"column:created_at;index"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"column:updated_at;index"`
}

// TableName 指定表名
func (GCPAccount) TableName() string {
	return "gcp_accounts"
}

type GcpAccountDao struct{}

var GGcpAccountDao = &GcpAccountDao{}

// Create 创建GCP账户
func (d *GcpAccountDao) Create(c *gin.Context, account *GCPAccount) error {
	return helpers.GatcDbClient.Create(account).Error
}

// GetByEmail 根据邮箱获取GCP账户
func (d *GcpAccountDao) GetByEmail(c *gin.Context, email string) (*GCPAccount, error) {
	var account GCPAccount
	err := helpers.GatcDbClient.Where("email = ?", email).First(&account).Error
	return &account, err
}

// List 查询GCP账户列表
func (d *GcpAccountDao) List(c *gin.Context, status int, offset, limit int) ([]GCPAccount, int64, error) {
	var accounts []GCPAccount
	var total int64

	query := helpers.GatcDbClient.Model(&GCPAccount{})
	if status > 0 {
		query = query.Where("auth_status = ?", status)
	}

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	err = query.Offset(offset).Limit(limit).Find(&accounts).Error
	return accounts, total, err
}

// Save 保存GCP账户
func (d *GcpAccountDao) Save(c *gin.Context, account *GCPAccount) error {
	return helpers.GatcDbClient.Save(account).Error
}

// GetByEmailAndProject 根据邮箱和项目ID获取账户
func (d *GcpAccountDao) GetByEmailAndProject(c *gin.Context, email, projectID string) (*GCPAccount, error) {
	var account GCPAccount
	err := helpers.GatcDbClient.Where("email = ? AND project_id = ?", email, projectID).First(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// GetUnboundProjectsByEmail 获取指定邮箱下所有未绑卡的项目
func (d *GcpAccountDao) GetUnboundProjectsByEmail(c *gin.Context, email string) ([]GCPAccount, error) {
	var accounts []GCPAccount
	err := helpers.GatcDbClient.Where("email = ? AND billing_status = ?", email, BillingStatusUnbound).Find(&accounts).Error
	return accounts, err
}

// GetBoundProjectsWithoutToken 获取已绑卡但没有token的项目
func (d *GcpAccountDao) GetBoundProjectsWithoutToken(c *gin.Context, email string) ([]GCPAccount, error) {
	var accounts []GCPAccount
	err := helpers.GatcDbClient.Where("email = ? AND billing_status = ? AND ( official_token = '')",
		email, BillingStatusBound).Find(&accounts).Error
	return accounts, err
}

// GetAccountStatus 获取账号状态记录（projectID为空的记录）
func (d *GcpAccountDao) GetAccountStatus(c *gin.Context, email string) (*GCPAccount, error) {
	var account GCPAccount
	err := helpers.GatcDbClient.Where("email = ? AND ( project_id = '')", email).First(&account).Error
	if err != nil {
		return nil, err
	}
	return &account, nil
}

// CreateOrUpdateAccountStatus 创建或更新账号状态记录
func (d *GcpAccountDao) CreateOrUpdateAccountStatus(c *gin.Context, email string, vmID string, authStatus int, debugInfo string) error {
	var account GCPAccount
	err := helpers.GatcDbClient.Where("email = ? AND ( project_id = '')", email).First(&account).Error

	if err != nil {
		// 记录不存在，创建新记录
		account = GCPAccount{
			Email:         email,
			ProjectID:     "", // 空表示账号状态记录
			VMID:          vmID,
			AuthStatus:    authStatus,
			AuthDebugInfo: debugInfo,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		return helpers.GatcDbClient.Create(&account).Error
	} else {
		// 记录存在，更新状态
		account.VMID = vmID
		account.AuthStatus = authStatus
		account.AuthDebugInfo = debugInfo
		account.UpdatedAt = time.Now()
		return helpers.GatcDbClient.Save(&account).Error
	}
}

// GetProjectsByEmail 获取指定邮箱下的所有项目记录（projectID非空）
func (d *GcpAccountDao) GetProjectsByEmail(c *gin.Context, email string) ([]GCPAccount, error) {
	var accounts []GCPAccount
	err := helpers.GatcDbClient.Where("email = ? AND project_id IS NOT NULL AND project_id != ''", email).Find(&accounts).Error
	return accounts, err
}

// SetTokenInvalid 设置token失效状态（通过ID）
func (d *GcpAccountDao) SetTokenInvalid(c *gin.Context, id int64) error {
	return helpers.GatcDbClient.Model(&GCPAccount{}).Where("id = ?", id).Updates(map[string]interface{}{
		"token_status": TokenStatusInvalid,
		"updated_at":   time.Now(),
	}).Error
}

// SetTokenInvalidByEmailAndProject 设置token失效状态（通过email和projectID）
func (d *GcpAccountDao) SetTokenInvalidByEmailAndProject(c *gin.Context, email, projectID string) error {
	return helpers.GatcDbClient.Model(&GCPAccount{}).Where("email = ? AND project_id = ?", email, projectID).Updates(map[string]interface{}{
		"token_status": TokenStatusInvalid,
		"updated_at":   time.Now(),
	}).Error
}

// GetEmailsWithUnboundProjects 获取包含未绑账单项目的所有邮箱
func (d *GcpAccountDao) GetEmailsWithUnboundProjects(c *gin.Context) ([]string, error) {
	var emails []string
	err := helpers.GatcDbClient.WithContext(c).Model(&GCPAccount{}).
		Select("DISTINCT email").
		Where("billing_status = ? AND project_id IS NOT NULL AND project_id != ''", BillingStatusUnbound).
		Pluck("email", &emails).Error
	return emails, err
}
