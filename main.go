package main

import (
	"fmt"
	"gatc/base/config"
	"gatc/base/middleware"
	"gatc/base/zlog"
	"gatc/conf"
	"gatc/dao"
	"gatc/handler"
	"gatc/helpers"
	"strconv"
	"time"

	"gatc/env"
	"github.com/gin-gonic/gin"
)

func main() {
	var err error

	// 初始化环境信息
	err = env.Init()
	if err != nil {
		panic("Failed to init env, error" + err.Error())
	}

	confPath := "./conf"
	if env.DevLocalEnv {
		confPath += "/dev"
	}
	fmt.Println("confPath: ", confPath)
	conf.LoadAppConfig(confPath + "/conf.yaml")
	conf.LoadResourceConf(confPath + "/resource.yaml")
	zlog.InitLogger(conf.AppConf.LogConf)
	helpers.InitMysql() // 各db 一个 gorm client

	// 初始化数据库表
	if err := helpers.GatcDbClient.AutoMigrate(
		&dao.VMInstance{},
		&dao.GCPAccount{},
	); err != nil {
		zlog.Error("Failed to migrate database", err)
		panic("Failed to migrate database: " + err.Error())
	}

	// 初始化GCP配置（SSH密钥和项目ID）
	if err := config.InitGCPConfig(); err != nil {
		zlog.Error("Failed to initialize GCP config", err)
		panic("Failed to initialize GCP config: " + err.Error())
	}

	r := gin.Default()

	// 添加请求ID中间件
	r.Use(middleware.RequestID())

	// Setup routes
	setupRoutes(r)

	err = r.Run(":" + strconv.Itoa(conf.AppConf.Port))
	if err != nil {
		zlog.Error("Failed to start server, err:", err)
		return
	}
}

func setupRoutes(r *gin.Engine) {
	// 健康检查路由
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "healthy",
			"service": "gatc",
			"timestamp": time.Now().Unix(),
		})
	})
	
	// VM管理路由
	vmHandler := handler.NewVMHandler()
	// 账户管理路由
	accountHandler := handler.NewAccountHandler()

	api := r.Group("/api/v1")
	{
		vm := api.Group("/vm")
		{
			vm.POST("/create", vmHandler.CreateVM)
			vm.POST("/delete", vmHandler.DeleteVM)
			vm.GET("/list", vmHandler.ListVMs)
			vm.GET("/get", vmHandler.GetVM)
			vm.POST("/refresh-ip", vmHandler.RefreshVMIP)
		}

		account := api.Group("/account")
		{
			//account.POST("/start-registration", accountHandler.StartRegistration)
			account.GET("/start-registration", accountHandler.StartRegistration)
			//account.POST("/submit-auth-key", accountHandler.SubmitAuthKey)
			account.GET("/submit-auth-key", accountHandler.SubmitAuthKey) // 支持GET回调
			account.GET("/list", accountHandler.ListAccounts)
			account.GET("/process-projects-v2", accountHandler.ProcessProjectsV2)                     // 项目处理流程V2（新的5步流程），参数：email
			account.POST("/set-token-invalid", accountHandler.SetTokenInvalid)                        // 设置token失效，参数：id 或 email+project_id
			account.GET("/set-token-invalid", accountHandler.SetTokenInvalid)                         // 设置token失效，支持GET请求
			account.GET("/emails-with-unbound-projects", accountHandler.GetEmailsWithUnboundProjects) // 获取包含未绑账单项目的邮箱列表
		}
	}
}
