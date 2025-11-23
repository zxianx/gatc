package gcloud

import (
	"context"
	"errors"
	"fmt"
	"gatc/base/zlog"
	"gatc/constants"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type AuthSessionSessionCache struct {
	sessions map[string]*AuthSession
	mutex    sync.RWMutex
}

var GAuthSessionSessionCache = &AuthSessionSessionCache{
	sessions: make(map[string]*AuthSession),
}

func (sm *AuthSessionSessionCache) GetAuthSession(sessionID string) (*AuthSession, bool) {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	session, exists := sm.sessions[sessionID]
	return session, exists
}

// RemoveAuthSession 手动删除会话（用于项目处理完成后清理）
func (sm *AuthSessionSessionCache) RemoveAuthSession(sessionID string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	delete(sm.sessions, sessionID)
}

type AuthStatus int

const (
	AuthSessionStatusNone       AuthStatus = 0
	AuthSessionStatusBeginLogin AuthStatus = 2
	AuthSessionStatusWaitKey    AuthStatus = 3
	AuthSessionStatusGetKey     AuthStatus = 4
	AuthSessionStatusDone       AuthStatus = 10
	AuthSessionStatusFail       AuthStatus = 11
)

// AuthSession 认证会话
type AuthSession struct {
	Ctx         *WorkCtx
	Status      AuthStatus         // 登陆状态:
	Msg         string             // 错误/成功/提示 信息
	DeadlineCtx context.Context    // 超时上下文
	Cancel      context.CancelFunc // 取消函数

	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	outputCh chan string
	inputCh  chan string
	doneCh   chan struct{}

	// SSH命令相关
	SSHCmd *exec.Cmd // login SSH命令,

}

func NewAuthLoginSession(ctx *WorkCtx) (session *AuthSession, err error) {
	dealineCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

	cmd := exec.CommandContext(dealineCtx,
		"ssh",
		"-i", constants.SSHKeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		fmt.Sprintf("%s@%s", ctx.VMInstance.SSHUser, ctx.VMInstance.ExternalIP),
		"gcloud auth login --no-launch-browser",
	)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	stdin, _ := cmd.StdinPipe()

	res := &AuthSession{
		Ctx:         ctx,
		Status:      0,
		Msg:         "",
		DeadlineCtx: dealineCtx,
		Cancel:      nil,
		stdin:       stdin,
		stdout:      stdout,
		stderr:      stderr,
		outputCh:    make(chan string, 100),
		inputCh:     make(chan string, 2),
		doneCh:      make(chan struct{}),
		SSHCmd:      cmd,
	}
	res.Cancel = func() {
		cancel()
		// 注意：登录成功后不自动删除session，保留给后续项目处理使用
		// 只关闭IO流，保留会话缓存
		res.stdin.Close()
		res.stdout.Close()
		res.stderr.Close()
	}

	GAuthSessionSessionCache.mutex.Lock()
	defer GAuthSessionSessionCache.mutex.Unlock()
	// 检查会话是否已存在
	if _, exists := GAuthSessionSessionCache.sessions[ctx.SessionID]; exists {
		return nil, errors.New("有正在登录中的任务 " + ctx.SessionID)
	}
	GAuthSessionSessionCache.sessions[ctx.SessionID] = res

	go func() {
		res.startInputWriter()
	}()
	return res, nil
}

// DoLogin 登录请求调用
func (as *AuthSession) DoLogin() (loginUrl string, err error) {
	zlog.InfoWithCtx(as.Ctx.GinCtx, "run cmd:", as.SSHCmd.String())
	// 执行交互登录命令
	// 若遇到 "Do you want to continue (Y/n)?" , 填写 Y\n
	// 获取输出的URL，返回。 （此时命令还没结束，等用户拿url到浏览器登陆获取token， 回填）

	as.Status = AuthSessionStatusBeginLogin

	// 启动命令
	if err := as.SSHCmd.Start(); err != nil {
		as.Status = AuthSessionStatusFail
		as.Msg = "启动 ssh/gcloud 命令失败: " + err.Error()
		zlog.ErrorWithCtx(as.Ctx.GinCtx, "SSH命令启动失败", err)
		return "", err
	}
	
	zlog.InfoWithCtx(as.Ctx.GinCtx, "SSH命令已启动，进程ID:", as.SSHCmd.Process.Pid)

	// 启动合并的输出读取器 (stdout + stderr)
	zlog.InfoWithCtx(as.Ctx.GinCtx, "启动合并输出读取器")
	go as.readCombinedOutput(as.stdout, as.stderr)
	
	// 短暂等待，确保读取器已启动
	time.Sleep(100 * time.Millisecond)
	zlog.InfoWithCtx(as.Ctx.GinCtx, "开始监听命令输出...")

	// 处理登录输出，实时响应交互提示和URL提取
	for {
		select {
		case line, ok := <-as.outputCh:
			zlog.InfoWithCtx(as.Ctx.GinCtx, "收到输出数据", "ok", ok, "line长度", len(line))
			if !ok {
				as.Status = AuthSessionStatusFail
				as.Msg = "输出通道关闭"
				zlog.ErrorWithCtx(as.Ctx.GinCtx, "输出通道意外关闭", nil)
				return "", errors.New("命令输出异常结束")
			}
			
			// 去掉换行符进行处理
			line = strings.TrimSpace(line)
			if line != "" {
				zlog.InfoWithCtx(as.Ctx.GinCtx, "[LOGIN STDOUT] "+line)
			}
			
			// 检查是否需要确认继续（现在来自stderr）
			if strings.Contains(line, "Do you want to continue (Y/n)?") || 
			   strings.Contains(line, "(Y/n)?") {
				zlog.InfoWithCtx(as.Ctx.GinCtx, "检测到确认提示，发送 Y", "来源", line)
				select {
				case as.inputCh <- "Y\n":
					zlog.InfoWithCtx(as.Ctx.GinCtx, "确认Y已发送")
				case <-as.DeadlineCtx.Done():
					return "", errors.New("发送确认超时")
				}
				continue
			}
			
			// 查找登录URL（通常来自stdout）
			if strings.Contains(line, "https://accounts.google.com/o/oauth2/auth") {
				// 提取URL - 先移除前缀
				cleanLine := line
				if strings.HasPrefix(line, "[STDOUT] ") {
					cleanLine = strings.TrimPrefix(line, "[STDOUT] ")
				} else if strings.HasPrefix(line, "[STDERR] ") {
					cleanLine = strings.TrimPrefix(line, "[STDERR] ")
				}
				
				parts := strings.Fields(cleanLine)
				for _, part := range parts {
					if strings.HasPrefix(part, "https://accounts.google.com/o/oauth2/auth") {
						loginUrl = part
						as.Status = AuthSessionStatusWaitKey
						as.Msg = "等待用户回填登录 token"
						zlog.InfoWithCtx(as.Ctx.GinCtx, "提取到登录URL: "+loginUrl)
						return loginUrl, nil
					}
				}
			}

		case <-as.DeadlineCtx.Done():
			as.Status = AuthSessionStatusFail
			as.Msg = "登录准备阶段超时"
			zlog.ErrorWithCtx(as.Ctx.GinCtx, "登录流程超时", nil)
			return "", errors.New("login waiting timeout")

		case <-time.After(30 * time.Second):
			// 30秒无输出，记录状态但继续等待
			zlog.InfoWithCtx(as.Ctx.GinCtx, "30秒无输出，检查进程状态", "进程状态", as.SSHCmd.ProcessState)
			// 检查进程是否还在运行
			if as.SSHCmd.ProcessState != nil {
				as.Status = AuthSessionStatusFail
				as.Msg = "SSH进程意外退出"
				zlog.ErrorWithCtx(as.Ctx.GinCtx, "SSH进程已退出", nil)
				return "", errors.New("SSH process exited unexpectedly")
			}
		}
	}

}

// CompleteLoginToken 登陆token回调请求调用
func (as *AuthSession) CompleteLoginToken(token string) error {
	if as.Status != AuthSessionStatusWaitKey {
		return fmt.Errorf("当前状态不允许回填token，当前状态: %d", as.Status)
	}

	zlog.InfoWithCtx(as.Ctx.GinCtx, "开始回填登录token")
	as.Status = AuthSessionStatusGetKey

	// 写入 token
	select {
	case as.inputCh <- token + "\n":
		zlog.InfoWithCtx(as.Ctx.GinCtx, "token已发送到输入通道")
	case <-as.DeadlineCtx.Done():
		as.Status = AuthSessionStatusFail
		as.Msg = "发送token超时"
		return errors.New("发送token超时")
	}

	// 等命令完整执行完
	err := as.waitCommandEnd()
	if err != nil {
		as.Status = AuthSessionStatusFail
		as.Msg = "gcloud 命令执行失败: " + err.Error()
		return err
	}

	as.Status = AuthSessionStatusDone
	as.Msg = "登录成功"
	zlog.InfoWithCtx(as.Ctx.GinCtx, "登录流程完成")
	return nil
}

// readCombinedOutput 合并读取stdout和stderr，解决交互提示在stderr的问题
func (as *AuthSession) readCombinedOutput(stdout, stderr io.ReadCloser) {
	defer func() {
		zlog.InfoWithCtx(as.Ctx.GinCtx, "readCombinedOutput 结束，关闭输出通道")
		close(as.outputCh)
		stdout.Close()
		stderr.Close()
	}()

	zlog.InfoWithCtx(as.Ctx.GinCtx, "readCombinedOutput 开始读取")
	
	// 启动两个goroutine分别读取stdout和stderr
	combinedCh := make(chan string, 100)
	
	// 读取stdout
	go func() {
		defer func() {
			zlog.InfoWithCtx(as.Ctx.GinCtx, "stdout读取器结束")
		}()
		
		buffer := make([]byte, 1)
		var accumulator strings.Builder
		
		for {
			select {
			case <-as.DeadlineCtx.Done():
				return
			default:
				n, err := stdout.Read(buffer)
				if err != nil {
					if err == io.EOF {
						if accumulator.Len() > 0 {
							combinedCh <- "[STDOUT] " + accumulator.String()
						}
						zlog.InfoWithCtx(as.Ctx.GinCtx, "stdout读取完成 (EOF)")
					} else {
						zlog.ErrorWithCtx(as.Ctx.GinCtx, "读取stdout错误: "+err.Error(), err)
					}
					return
				}
				
				if n > 0 {
					char := string(buffer[:n])
					accumulator.WriteString(char)
					
					// 遇到换行符发送整行
					if char == "\n" {
						combinedCh <- "[STDOUT] " + accumulator.String()
						accumulator.Reset()
					}
				}
			}
		}
	}()
	
	// 读取stderr
	go func() {
		defer func() {
			zlog.InfoWithCtx(as.Ctx.GinCtx, "stderr读取器结束")
		}()
		
		buffer := make([]byte, 1)
		var accumulator strings.Builder
		
		for {
			select {
			case <-as.DeadlineCtx.Done():
				return
			default:
				n, err := stderr.Read(buffer)
				if err != nil {
					if err == io.EOF {
						if accumulator.Len() > 0 {
							combinedCh <- "[STDERR] " + accumulator.String()
						}
						zlog.InfoWithCtx(as.Ctx.GinCtx, "stderr读取完成 (EOF)")
					} else {
						zlog.ErrorWithCtx(as.Ctx.GinCtx, "读取stderr错误: "+err.Error(), err)
					}
					return
				}
				
				if n > 0 {
					char := string(buffer[:n])
					accumulator.WriteString(char)
					
					// 检查是否遇到换行符或特殊提示
					if char == "\n" {
						combinedCh <- "[STDERR] " + accumulator.String()
						accumulator.Reset()
					} else {
						// 检查半行提示 (Y/n)?
						content := accumulator.String()
						if strings.Contains(content, "(Y/n)?") {
							zlog.InfoWithCtx(as.Ctx.GinCtx, "检测到stderr确认提示（半行）", "内容", content)
							combinedCh <- "[STDERR] " + content
							accumulator.Reset()
						}
					}
				}
			}
		}
	}()
	
	// 统一处理合并后的输出
	for {
		select {
		case <-as.DeadlineCtx.Done():
			zlog.InfoWithCtx(as.Ctx.GinCtx, "readCombinedOutput 上下文取消")
			return
		case line, ok := <-combinedCh:
			if !ok {
				return
			}
			
			zlog.InfoWithCtx(as.Ctx.GinCtx, "合并输出", "内容", line)
			
			// 发送到主通道
			select {
			case as.outputCh <- line:
			case <-as.DeadlineCtx.Done():
				return
			}
		}
	}
}


//func (as *AuthSession) readPipeInterval( r io.ReadCloser) {
//	defer close(as.outputCh)
//
//	buf := make([]byte, 4096)
//	var pending bytes.Buffer
//
//	ticker := time.NewTicker(100 * time.Millisecond)
//	defer ticker.Stop()
//
//	for {
//		select {
//		case <-as.DeadlineCtx.Done():
//			return
//		default:
//		}
//
//		// 尝试尽可能多读（管道无数据会立即返回 n=0,nil != EOF）
//		for {
//			r.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
//
//			n, err := r.Read(buf)
//			if n > 0 {
//				pending.Write(buf[:n])
//			}
//
//			if err != nil {
//				if errors.Is(err, os.ErrDeadlineExceeded) {
//					// 当前没有更多数据可读，跳出 read 循环
//					break
//				}
//				if err == io.EOF {
//					// 完结，输出剩余部分
//					if pending.Len() > 0 {
//						as.splitAndSend(&pending)
//					}
//					return
//				}
//				// 其他错误，退出
//				return
//			}
//
//			// 若 n < len(buf)，说明暂时读完了
//			if n < len(buf) {
//				break
//			}
//		}
//
//		// 每 100ms 处理一次已读取的数据
//		select {
//		case <-ticker.C:
//			as.splitAndSend(&pending)
//		default:
//		}
//	}
//}


// 统一写入 stdin
func (as *AuthSession) startInputWriter() {
	go func() {
		defer func() {
			if as.stdin != nil {
				as.stdin.Close()
			}
		}()
		
		for {
			select {
			case v, ok := <-as.inputCh:
				if !ok {
					// inputCh已关闭
					return
				}
				if as.stdin != nil {
					_, err := as.stdin.Write([]byte(v))
					if err != nil {
						zlog.InfoWithCtx(as.Ctx.GinCtx, "写入stdin失败: "+err.Error())
						return
					}
				}
			case <-as.DeadlineCtx.Done():
				return
			}
		}
	}()
}

func (as *AuthSession) waitCommandEnd() error {
	// 等待进程退出
	err := as.SSHCmd.Wait()

	// 关闭输入输出
	close(as.inputCh)

	if err != nil {
		return err
	}
	return nil
}
