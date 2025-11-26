package dao

import (
	"errors"
	"fmt"
	"gatc/helpers"
	"gorm.io/gorm"
	"time"

	"github.com/gin-gonic/gin"
)

type GormOfficialTokens struct {
	Id                int64     `gorm:"column:id;primaryKey;autoIncrement;not null" json:"id"`
	ChannelId         int64     `gorm:"column:channel_id;not null" json:"channel_id"`                                //'所属渠道ID'
	Name              string    `gorm:"column:name" json:"name,omitempty"`                                           //'Token名称'
	Token             string    `gorm:"column:token;not null" json:"token"`                                          //'API密钥'
	BaseUrl           string    `gorm:"column:base_url" json:"base_url,omitempty"`                                   //'Token专用BaseURL，为空时使用Channel的BaseURL'
	Status            int64     `gorm:"column:status;default:1" json:"status,omitempty"`                             //'状态 1:正常 2:暂停 3:异常'
	Priority          int64     `gorm:"column:priority;default:50" json:"priority,omitempty"`                        //'Token优先级'
	Weight            int64     `gorm:"column:weight;default:100" json:"weight,omitempty"`                           //'Token权重'
	RpmLimit          int64     `gorm:"column:rpm_limit;default:0" json:"rpm_limit,omitempty"`                       //'Token级RPM限制'
	TpmLimit          int64     `gorm:"column:tpm_limit;default:0" json:"tpm_limit,omitempty"`                       //'Token级TPM限制'
	TotalRequests     int64     `gorm:"column:total_requests;default:0" json:"total_requests,omitempty"`             //'总请求数'
	SuccessRequests   int64     `gorm:"column:success_requests;default:0" json:"success_requests,omitempty"`         //'成功请求数'
	FailedRequests    int64     `gorm:"column:failed_requests;default:0" json:"failed_requests,omitempty"`           //'失败请求数'
	TotalInputTokens  int64     `gorm:"column:total_input_tokens;default:0" json:"total_input_tokens,omitempty"`     //'总输入token数'
	TotalOutputTokens int64     `gorm:"column:total_output_tokens;default:0" json:"total_output_tokens,omitempty"`   //'总输出token数'
	FailureReason     string    `gorm:"column:failure_reason" json:"failure_reason,omitempty"`                       //'失效原因'
	LastUsedAt        time.Time `gorm:"column:last_used_at;default:CURRENT_TIMESTAMP" json:"last_used_at,omitempty"` //'最后使用时间'
	CreatedAt         time.Time `gorm:"column:created_at;default:CURRENT_TIMESTAMP" json:"created_at,omitempty"`
	UpdatedAt         time.Time `gorm:"column:updated_at;default:CURRENT_TIMESTAMP" json:"updated_at,omitempty"`
	CustomAuthType    string    `gorm:"column:custom_auth_type" json:"custom_auth_type,omitempty"`                           //'自定义认证方式:header/bearer/query'
	CustomHeaderName  string    `gorm:"column:custom_header_name" json:"custom_header_name,omitempty"`                       //'自定义header名称(header认证时)'
	CustomQueryParam  string    `gorm:"column:custom_query_param" json:"custom_query_param,omitempty"`                       //'自定义参数名(query认证时)'
	Proxy             string    `gorm:"column:proxy" json:"proxy,omitempty"`                                                 //'代理地址，如: http://127.0.0.1:7890 或 socks5://127.0.0.1:7891'
	ExternalSource    string    `gorm:"column:external_source" json:"external_source,omitempty"`                             //'外部数据源标识,如newapi'
	ExternalId        string    `gorm:"column:external_id" json:"external_id,omitempty"`                                     //'外部系统ID'
	ExternalSyncAt    time.Time `gorm:"column:external_sync_at;default:CURRENT_TIMESTAMP" json:"external_sync_at,omitempty"` //'最后外部同步时间'
	TokenType         string    `gorm:"column:token_type;default:'static'" json:"token_type,omitempty"`                      //'令牌类型:static/oauth2'
	OAuth2Config      string    `gorm:"column:o_auth2_config" json:"o_auth2_config,omitempty"`                               //'OAuth2配置JSON(Service Account信息)'
	RuntimeToken      string    `gorm:"column:runtime_token" json:"runtime_token,omitempty"`                                 //'OAuth2运行时访问令牌'
	TokenExpiresAt    time.Time `gorm:"column:token_expires_at;default:CURRENT_TIMESTAMP" json:"token_expires_at,omitempty"` //'OAuth2令牌过期时间'
	RefreshStatus     int64     `gorm:"column:refresh_status;default:0" json:"refresh_status,omitempty"`                     //'刷新状态:0=就绪,1=失败'
	LastRefreshError  string    `gorm:"column:last_refresh_error" json:"last_refresh_error,omitempty"`                       //'最后刷新错误信息'
	Oauth2Config      string    `gorm:"column:oauth2_config" json:"oauth2_config,omitempty"`                                 //OAuth2配置(新字段)
	Email             string    `gorm:"column:email" json:"email,omitempty"`
	ProjectId         string    `gorm:"column:project_id" json:"project_id,omitempty"`
}

func (c *GormOfficialTokens) getDb() *gorm.DB {
	return helpers.GatcDbClient
}
func (c *GormOfficialTokens) TableName() string {
	return "official_tokens"
}

func (c *GormOfficialTokens) ExistByPk(ctx *gin.Context) (exist bool, err error) {

	count := 0
	err = c.getDb().WithContext(ctx).Raw("select count(1) from TABLENAME where id  = ? ", c.Id).Scan(&count).Error
	exist = count > 0
	return
}

func (c *GormOfficialTokens) Save(ctx *gin.Context) (err error) {
	// 直接save会保留所有字段 包括空字段
	if c.Id == 0 {
		return c.Create(ctx)
	} else {
		return c.UpdateByPk(ctx)
	}
	/*
	       // 无法根据有无id判断插入更新情况
	   	 exist, err := c.ExistByPk(ctx)
	       if err != nil {
	           return err
	       }
	       if exist {
	           return c.UpdateByPk(ctx)
	       } else {
	           return c.Create(ctx)
	       }

	*/

}

func (c *GormOfficialTokens) GetByPk(ctx *gin.Context, selects string) (err error) {
	if c.Id == 0 {
		err = errors.New("empty Id")
		return
	}
	db := c.getDb()
	if ctx != nil {
		db = db.WithContext(ctx)
	}
	if selects == "" {
		selects = "*"
	}
	err = db.Table(c.TableName()).Select(selects).First(c, c.Id).Error
	return
}

func (c *GormOfficialTokens) GetOne(ctx *gin.Context, selects, extraWhereCond, order string) (err error) {
	db := c.getDb()
	if ctx != nil {
		db = db.WithContext(ctx)
	}
	if selects == "" {
		selects = "*"
	}
	if extraWhereCond != "" {
		db = db.Where(extraWhereCond)
	}
	if order != "" {
		db = db.Order(order)
	}
	err = db.Table(c.TableName()).Select(selects).Where(&c).First(&c).Error
	return
}

func (c *GormOfficialTokens) GetList(ctx *gin.Context, selects, extraWhereCond, order string, limit, offset int) (res []GormOfficialTokens, err error) {
	db := c.getDb()
	if ctx != nil {
		db = db.WithContext(ctx)
	}
	db = db.Table(c.TableName()).Where(&c)
	if extraWhereCond != "" {
		db = db.Where(extraWhereCond)
	}
	if selects != "" {
		db = db.Select(selects)
	}
	if limit != 0 {
		db = db.Limit(limit).Offset(offset)
		if order == "" {
			order = "id"
		}
	}
	if order != "" {
		db = db.Order(order)
	}
	err = db.Find(&res).Error
	return
}

func (c *GormOfficialTokens) GetRowsByIds(ctx *gin.Context, idStr string, selects string) (itemList []GormOfficialTokens, err error) {
	db := c.getDb()
	if ctx != nil {
		db = db.WithContext(ctx)
	}
	if selects == "" {
		selects = "*"
	}
	err = db.WithContext(ctx).Where(fmt.Sprintf("id in (%s)", idStr)).Select(selects).Find(&itemList).Error
	return
}

func (c *GormOfficialTokens) Create(ctx *gin.Context) (err error) {
	db := c.getDb()
	if ctx != nil {
		db = db.WithContext(ctx)
	}
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	err = db.Model(&c).Create(&c).Error

	return
}

// UpdateByPk  更新单条记录推荐， 避免意外参数错误Update批量错误更新
func (c *GormOfficialTokens) UpdateByPk(ctx *gin.Context, updateFields ...string) (err error) {
	if c.Id == 0 {
		err = errors.New("empty id")
		return
	}

	c.UpdatedAt = time.Now()
	db := c.getDb()
	if ctx != nil {
		db = db.WithContext(ctx)
	}
	if len(updateFields) != 0 {
		updateFields = append(updateFields, "utime")
		db = db.Select(updateFields)
	}
	err = db.Table(c.TableName()).Where("id = ?", c.Id).Updates(&c).Error
	return
}

func (c *GormOfficialTokens) Updates(ctx *gin.Context, cond *GormOfficialTokens, condRaw string, updateFields []string, limit int) (err error) {
	c.UpdatedAt = time.Now()
	db := c.getDb()
	if ctx != nil {
		db = db.WithContext(ctx)
	}
	if len(updateFields) != 0 {
		updateFields = append(updateFields, "utime")
		db = db.Select(updateFields)
	}
	if condRaw != "" {
		db = db.Where(condRaw)
	}
	if cond != nil {
		db = db.Where(cond)
	}
	if limit != 0 {
		db = db.Limit(limit)
	}
	err = db.Table(c.TableName()).Updates(&c).Error
	return
}
