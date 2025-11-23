package gcloud

import (
	"gatc/dao"
	"github.com/gin-gonic/gin"
)

type WorkCtx struct {
	SessionID  string          // 会话ID
	Email      string          // 邮箱
	VMInstance *dao.VMInstance // VM实例
	GinCtx     *gin.Context
}
