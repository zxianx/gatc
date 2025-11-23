package helpers

import (
	"fmt"
	"gatc/conf"
	"gorm.io/gorm"
	// driver "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
)

var GatcDbClient *gorm.DB

func InitMysql() {
	var err error
	for name, dbConf := range conf.MysqlConfs {
		switch name {
		case "gatc":
			GatcDbClient, err = initMysqlClient(dbConf)
		}

		if err != nil {
			panic("mysql connect error: %v" + err.Error())
		}
	}
}

func initMysqlClient(conf conf.MysqlConf) (client *gorm.DB, err error) {
	dsn := ""
	if conf.Dsn != "" {
		dsn = conf.Dsn
	} else {
		dsn = fmt.Sprintf(
			"%s:%s@tcp(%s)/%s?timeout=%s&readTimeout=%s&writeTimeout=%s&parseTime=True&loc=Asia%%2FShanghai&interpolateParams=%s&charset=utf8mb4",
			conf.User,
			conf.Password,
			conf.Addr,
			conf.DataBase,
			conf.ConnTimeOut,
			conf.ReadTimeOut,
			conf.WriteTimeOut,
			conf.InterpolateParams, //false
		)
	}

	c := &gorm.Config{
		SkipDefaultTransaction: true,
		NamingStrategy:         nil,
		FullSaveAssociations:   false,
		//Logger:                                   l,
		NowFunc:                                  nil,
		DryRun:                                   false,
		PrepareStmt:                              false,
		DisableAutomaticPing:                     false,
		DisableForeignKeyConstraintWhenMigrating: false,
		AllowGlobalUpdate:                        false,
		ClauseBuilders:                           nil,
		ConnPool:                                 nil,
		Dialector:                                nil,
		Plugins:                                  nil,
	}

	client, err = gorm.Open(mysql.Open(dsn), c)
	if err != nil {
		return client, err
	}

	sqlDB, err := client.DB()
	if err != nil {
		return client, err
	}

	// 注册 mysql collector
	//if RuntimeMetricsRegister != nil {
	//	RuntimeMetricsRegister.MustRegister(collectors.NewDBStatsCollector(sqlDB, conf.Service))
	//}

	// SetMaxIdleConns 设置空闲连接池中连接的最大数量
	sqlDB.SetMaxIdleConns(conf.MaxIdleConns)

	// SetMaxOpenConns 设置打开数据库连接的最大数量
	sqlDB.SetMaxOpenConns(conf.MaxOpenConns)

	// SetConnMaxLifetime 设置了连接可复用的最大时间
	sqlDB.SetConnMaxLifetime(conf.ConnMaxLifeTime)

	// only for go version >= 1.15 设置最大空闲连接时间
	sqlDB.SetConnMaxIdleTime(conf.ConnMaxIdlTime)

	return client, nil
}
