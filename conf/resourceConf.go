package conf

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"time"
)

type MysqlConf struct {
	Service           string        `yaml:"service"`
	DataBase          string        `yaml:"database"`
	Addr              string        `yaml:"addr"`
	User              string        `yaml:"user"`
	Password          string        `yaml:"password"`
	MaxIdleConns      int           `yaml:"maxidleconns"`
	MaxOpenConns      int           `yaml:"maxopenconns"`
	ConnMaxIdlTime    time.Duration `yaml:"maxIdleTime"`
	ConnMaxLifeTime   time.Duration `yaml:"connMaxLifeTime"`
	ConnTimeOut       time.Duration `yaml:"connTimeOut"`
	WriteTimeOut      time.Duration `yaml:"writeTimeOut"`
	ReadTimeOut       time.Duration `yaml:"readTimeOut"`
	InterpolateParams string        `yaml:"interpolateParams"`
}

var MysqlConfs map[string]MysqlConf

func LoadResourceConf(path string) {
	fmt.Println("use conf file:", path)
	rcConf := struct {
		Mysql map[string]MysqlConf
	}{}

	if yamlFile, err := os.ReadFile(path); err != nil {
		panic(path + " get error: %v " + err.Error())
	} else if err = yaml.Unmarshal(yamlFile, &rcConf); err != nil {
		panic(path + "LoadResourceConf unmarshal error: %v" + err.Error())
	}
	MysqlConfs = rcConf.Mysql
	return
}
