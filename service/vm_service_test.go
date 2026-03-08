package service

import (
	"gatc/test_common"
	"gatc/tool"
	"testing"
)

func TestVMService_SyncVMsWithGCP(t *testing.T) {
	s := &VMService{}
	s.SyncVMsWithGCP()
}

func TestMain(m *testing.M) {
	tool.SetLocalProxy()
	test_common.Test_main_init(m)
}
