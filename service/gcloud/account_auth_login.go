package gcloud

import (
	"fmt"
	"gatc/base/zlog"
	"gatc/constants"
	"os/exec"
	"strings"
)

type AccountAuthStatus string

const (
	AccountAuthStatusInactive  AccountAuthStatus = "inactive"
	AccountAuthSStatusActive   AccountAuthStatus = "active"
	AccountAuthSStatusNotLogin AccountAuthStatus = "not_login"
)

func (ctx *WorkCtx) CheckTargetAccount() (AccountAuthStatus, error) {
	// 获取VM上所有已认证的账户
	listCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.VMInstance.SSHUser, ctx.VMInstance.ExternalIP),
		"gcloud auth list --format='value(account,status)'",
	)

	output, err := listCmd.Output()
	if err != nil {
		zlog.ErrorWithMsgAndCtx(ctx.GinCtx, "CheckTargetAccount fail, ", ctx.VMInstance.VMID, ctx.VMInstance.ExternalIP, "【CMD】", listCmd.String())
		return "", fmt.Errorf("failed to list accounts: %v  %s", err, output)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) >= 2 {
			email := parts[0]
			status := parts[1]

			// 跳过GCE服务账户
			if strings.Contains(email, "@developer.gserviceaccount.com") {
				continue
			}

			// 检查是否是目标邮箱
			if email == ctx.Email {
				if strings.ToUpper(status) == "ACTIVE" || status == "*" {
					return AccountAuthSStatusActive, nil
				} else {
					return AccountAuthStatusInactive, nil
				}
			}
		}
	}
	return AccountAuthSStatusNotLogin, nil
}

// SwitchToAccount 切换到指定账户
func (ctx *WorkCtx) SwitchToAccount() error {
	switchCmd := exec.Command(
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.VMInstance.SSHUser, ctx.VMInstance.ExternalIP),
		fmt.Sprintf("gcloud config set account %s", ctx.Email),
	)

	output, err := switchCmd.Output()
	zlog.Info("Switch account output for session, ", ctx.SessionID, string(output))

	if err != nil {
		return fmt.Errorf("failed to switch account: %v", err)
	}

	return nil
}
