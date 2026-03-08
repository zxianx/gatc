package cron

import (
	"fmt"
	"gatc/base/zlog"
	"time"

	"github.com/robfig/cron/v3"
)

var globalCron *cron.Cron

func Init() {
	globalCron = cron.New()
	zlog.Info("Cron scheduler initialized")
}

func Start() {
	if globalCron != nil {
		globalCron.Start()
		zlog.Info("Cron scheduler started")
	}
}

func Stop() {
	if globalCron != nil {
		globalCron.Stop()
		zlog.Info("Cron scheduler stopped")
	}
}

func AddFunc(name string, spec string, cmd func(), extra_once_run_delay time.Duration) {
	if globalCron == nil {
		Init()
	}
	_, err := globalCron.AddFunc(spec, cmd)
	if err != nil {
		panic(fmt.Sprintf("globalCron.AddFunc fail , name: %s, cron %s, err %v", name, spec, err))
	}
	if extra_once_run_delay >= 0 {
		go func() {
			time.Sleep(extra_once_run_delay)
			cmd()
		}()
	}
}

func Remove(id cron.EntryID) {
	if globalCron != nil {
		globalCron.Remove(id)
	}
}
