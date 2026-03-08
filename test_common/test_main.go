package test_common

import (
	"fmt"
	"gatc/base/config"
	"gatc/base/zlog"
	"gatc/conf"
	"gatc/env"
	"gatc/helpers"
	"github.com/gin-gonic/gin"
	"os"
	"path/filepath"
	"testing"
)

func Test_main_init(m *testing.M) {
	// 初始化环境
	if err := env.Init(); err != nil {
		panic("Failed to init env: " + err.Error())
	}

	Test_set_root_dir()

	// 使用测试配置路径
	confPath := "./conf"
	if env.DevLocalEnv {
		confPath += "/dev"
	}

	// 加载配置
	conf.LoadAppConfig(confPath + "/conf.yaml")
	conf.LoadResourceConf(confPath + "/resource.yaml")
	zlog.InitLogger(conf.AppConf.LogConf)

	// 初始化数据库
	helpers.InitMysql()

	if err := config.InitGCPConfig(); err != nil {
		zlog.Error("Failed to initialize GCP config", err)
		panic("Failed to initialize GCP config: " + err.Error())
	}

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

func Test_set_root_dir() {
	root := projectRoot()
	fmt.Println("project set root dir", root)
	os.Chdir(root)
}

func projectRoot() string {
	dir, _ := os.Getwd()

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}

		dir = parent
	}
}
