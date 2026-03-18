package dao

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"testing"
)

func TestGormOfficialTokens_DeleteGcpTokenByEmails(t *testing.T) {
	tmp := &GormOfficialTokens{}
	ctx := &gin.Context{}
	affect, delTokenErr := tmp.DeleteGcpTokenByEmails(ctx, []string{"danny@innov-concept-travaux.com", "test2@gmail.com"})
	fmt.Println(affect, delTokenErr)
}
