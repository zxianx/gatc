package zlog

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

var zlog *zap.SugaredLogger

type LoggConf struct {
	Level  string `yaml:"level" json:"level"`
	Output string `yaml:"output" json:"output"`
}

func InitLogger(conf LoggConf) {
	// 配置zap以正确显示caller信息
	config := zap.NewProductionConfig()
	config.DisableCaller = false
	log, _ := config.Build(zap.AddCallerSkip(1)) // 跳过一层调用栈
	zlog = log.Sugar()
}

// 原有的日志函数保持兼容
func Error(args ...interface{}) {
	zlog.Error(args)
}

func Info(args ...interface{}) {
	zlog.Info(args)
}

func Debug(args ...interface{}) {
	zlog.Debug(args)
}

func Warn(args ...interface{}) {
	zlog.Warn(args)
}

// 带RequestID的结构化日志函数

func DebugWithCtx(c *gin.Context, msg string, fields ...interface{}) {
	fields = appendRequestID(c, fields)
	zlog.Debugw(msg, fields...)
}

func InfoWithCtx(c *gin.Context, msg string, fields ...interface{}) {
	fields = appendRequestID(c, fields)
	zlog.Infow(msg, fields...)
}

func WarnWithCtx(c *gin.Context, msg string, fields ...interface{}) {
	fields = appendRequestID(c, fields)
	zlog.Warnw(msg, fields...)
}

func ErrorWithCtx(c *gin.Context, msg string, err error) {
	fields := []interface{}{"error", err}
	fields = appendRequestID(c, fields)
	zlog.Errorw(msg, fields...)
}

func ErrorWithMsgAndCtx(c *gin.Context, msg string, fields ...interface{}) {
	fields = appendRequestID(c, fields)
	zlog.Errorw(msg, fields...)
}

// appendRequestID 添加requestId到日志字段中
func appendRequestID(c *gin.Context, fields []interface{}) []interface{} {
	if c != nil {
		if requestID, exists := c.Get("X-Request-ID"); exists {
			fields = append(fields, "requestId", requestID)
		}
	}
	return fields
}
