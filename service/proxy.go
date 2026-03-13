package service

import (
	"fmt"
	"gatc/base/middleware"
	"gatc/base/zlog"
	"gatc/tool"
	"github.com/gin-gonic/gin"
)

func SyncProxyByVms() {
	c := &gin.Context{}
	c.Set(middleware.RequestIDKey, fmt.Sprintf("CRON SyncProxyByVms"))
	res, err := GVmService.SyncProxyPoolFromVMs(c)
	zlog.InfoWithCtx(c, fmt.Sprintf("SyncProxyByVms, err: %v , res: %s", err, tool.ToJson(res)))
}
