package test_common

import (
	"gatc/base/zlog"
	"gatc/conf"
	"gatc/env"
	"gatc/helpers"
	"github.com/gin-gonic/gin"
	"os"
	"testing"
)

func Test_main_init(m *testing.M) {
	// 初始化环境
	if err := env.Init(); err != nil {
		panic("Failed to init env: " + err.Error())
	}

	// 使用测试配置路径
	confPath := "../conf"
	if env.DevLocalEnv {
		confPath += "/dev"
	}

	// 加载配置
	conf.LoadAppConfig(confPath + "/conf.yaml")
	conf.LoadResourceConf(confPath + "/resource.yaml")
	zlog.InitLogger(conf.AppConf.LogConf)

	// 初始化数据库
	helpers.InitMysql()

	// 初始化数据库表
	//if err := helpers.GatcDbClient.AutoMigrate(
	//	&dao.VMInstance{},
	//	&dao.GCPAccount{},
	//); err != nil {
	//	panic("Failed to migrate database: " + err.Error())
	//}

	// 设置gin为测试模式
	gin.SetMode(gin.TestMode)

	// 运行测试
	code := m.Run()

	// 测试清理（可选）
	// 这里可以添加清理逻辑

	os.Exit(code)
}
