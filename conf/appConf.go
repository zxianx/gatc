package conf

import (
	"fmt"
	"gatc/base/zlog"
	jsoniter "github.com/json-iterator/go"
	"gopkg.in/yaml.v3"
	"os"
)

var AppConf = appConf{
	Port: 8080,
}

type appConf struct {
	Port    int           `yaml:"port" json:"port"`
	LogConf zlog.LoggConf `yaml:"log" json:"log"`
}

func LoadAppConfig(path string) {
	content, err := os.ReadFile(path)
	if err != nil {

		panic(fmt.Sprintf("LoadConfig read config file error: %v", err))
		return
	}
	err = yaml.Unmarshal(content, &AppConf)

	if err != nil {
		panic(fmt.Sprintf("LoadConfig parse config file error: %v", err))
	}

	confStr, _ := jsoniter.MarshalToString(AppConf)
	fmt.Println("LodConf " + confStr)
}
