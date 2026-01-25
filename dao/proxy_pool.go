package dao

/*

CREATE TABLE `proxy_pool` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `status` bigint NOT NULL DEFAULT '0',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `proxy` varchar(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '' COMMENT 'proxy地址',
  `proxy_type` varchar(16) CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci NOT NULL DEFAULT '' COMMENT 'server或socks5',
  PRIMARY KEY (`id`),
  UNIQUE KEY `idx_proxy_status` (`proxy`,`status`),
  KEY `idx_proxy_type_status` (`proxy_type`,`status`),
  KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB AUTO_INCREMENT=302 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

*/

/*
proxy_type = server 类型的行，  proxy 格式类似  "http://35.208.147.190:1081" (没有"/px" 后缀)
*/

import (
	"gatc/helpers"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	ProxyStatusInactive = 0
	ProxyStatusActive   = 1
	ProxyStatusOccupied = 2
	ProxyStatusDeleted  = 9
)

type ProxyPool struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Status    int64     `gorm:"column:status;not null;default:0" json:"status"`
	CreatedAt time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at" json:"updated_at"`
	Proxy     string    `gorm:"column:proxy;not null" json:"proxy"`
	ProxyType string    `gorm:"column:proxy_type;not null" json:"proxy_type"`
}

func (ProxyPool) TableName() string {
	return "proxy_pool"
}

type ProxyPoolDao struct{}

var GProxyPoolDao = &ProxyPoolDao{}

// GetLastBatchByType 按created_at倒序获取指定类型和状态的代理，limit个数
func (d *ProxyPoolDao) GetLastBatchByType(c *gin.Context, proxyType string, status int64, limit int) ([]ProxyPool, error) {
	var proxies []ProxyPool
	err := getDB(c).
		Where("proxy_type = ? AND status = ?", proxyType, status).
		Order("created_at DESC").
		Limit(limit).
		Find(&proxies).Error
	return proxies, err
}

// BatchCreate 批量创建代理记录
func (d *ProxyPoolDao) BatchCreate(c *gin.Context, proxies []ProxyPool) error {
	if len(proxies) == 0 {
		return nil
	}
	return getDB(c).Create(&proxies).Error
}

// BatchUpdateStatus 批量更新代理状态
func (d *ProxyPoolDao) BatchUpdateStatus(c *gin.Context, proxyIDs []int64, status int64) error {
	if len(proxyIDs) == 0 {
		return nil
	}
	return getDB(c).Model(&ProxyPool{}).
		Where("id IN ?", proxyIDs).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}).Error
}

// GetByProxy 根据proxy地址查询
func (d *ProxyPoolDao) GetByProxy(c *gin.Context, proxy string) (*ProxyPool, error) {
	var p ProxyPool
	err := getDB(c).Where("proxy = ?", proxy).First(&p).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return &p, err
}

func getDB(c *gin.Context) *gorm.DB {
	// 使用现有的数据库连接方式
	// 假设与其他DAO保持一致，使用helpers.GatcDbClient
	return helpers.GatcDbClient.WithContext(c)
}
