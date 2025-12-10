package dao

import (
	"fmt"
	"gatc/test_common"
	"github.com/gin-gonic/gin"
	"testing"
	"time"
)

func TestVMInstanceDao_GetVMsCreatedBefore(t *testing.T) {
	c := &gin.Context{}
	lTime := time.Now().Add(-24 * time.Hour)
	res, err := GVmInstanceDao.GetVMsCreatedBefore(c, lTime)
	fmt.Println(len(res), err)
}

func TestMain(m *testing.M) {
	test_common.Test_main_init(m)
}
