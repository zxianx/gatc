package gcloud

import (
	"fmt"
	"gatc/dao"
	"gatc/test_common"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMain 在所有测试运行前统一初始化（推荐方案）
func TestMain(m *testing.M) {
	test_common.Test_main_init(m)
}

// 轻量级测试初始化函数（TestMain已初始化大部分依赖）
func setupTest(t *testing.T) {
	// TestMain已经初始化了所有必要的依赖
	// 这里可以添加每个测试特有的初始化逻辑

	// 例如：清理测试数据
	// helpers.GatcDbClient.Exec("TRUNCATE TABLE official_tokens")
}

func Test_insertOfficialToken(t *testing.T) {
	// 初始化测试环境
	setupTest(t)

	// 创建gin context用于测试
	ctx := &gin.Context{}

	// 创建测试用的GCPAccount
	testProject := dao.GCPAccount{
		ID:            101,
		Email:         "test@example.com",
		ProjectID:     "test-project-123",
		OfficialToken: "AIzaSyTest1234567890",
		Sock5Proxy:    "socks5://127.0.0.1:1080",
	}

	// 调用insertOfficialToken函数
	tokenId, err := insertOfficialToken(ctx, "test@example.com", testProject)

	// 验证结果
	if err != nil {
		t.Errorf("insertOfficialToken failed: %v", err)
	}

	if tokenId == 0 {
		t.Error("Expected non-zero tokenId")
	}

	t.Logf("Successfully inserted token with ID: %d", tokenId)

	err3 := updateOfficialTokenIdDirect(ctx, &testProject, tokenId)
	fmt.Println(err3)

}
