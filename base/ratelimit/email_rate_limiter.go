package ratelimit

import (
	"sync"
	"time"
)

// EmailRateLimiter 邮箱请求频率限制器
type EmailRateLimiter struct {
	mu           sync.RWMutex
	requests     map[string]time.Time // email -> 最后请求时间
	intervalTime time.Duration        // 限制间隔时间
}

// NewEmailRateLimiter 创建新的邮箱频率限制器
func NewEmailRateLimiter(interval time.Duration) *EmailRateLimiter {
	return &EmailRateLimiter{
		requests:     make(map[string]time.Time),
		intervalTime: interval,
	}
}

// CanProcess 检查指定邮箱是否可以处理请求
// 返回 (是否可以处理, 距离下次可处理的剩余时间)
func (rl *EmailRateLimiter) CanProcess(email string) (bool, time.Duration) {
	rl.mu.RLock()
	lastTime, exists := rl.requests[email]
	rl.mu.RUnlock()

	now := time.Now()
	
	if !exists {
		// 首次请求，允许处理
		rl.recordRequest(email, now)
		return true, 0
	}

	// 计算时间差
	elapsed := now.Sub(lastTime)
	if elapsed >= rl.intervalTime {
		// 超过限制间隔，允许处理
		rl.recordRequest(email, now)
		return true, 0
	}

	// 未超过限制间隔，拒绝处理
	remaining := rl.intervalTime - elapsed
	return false, remaining
}

// recordRequest 记录请求时间
func (rl *EmailRateLimiter) recordRequest(email string, reqTime time.Time) {
	rl.mu.Lock()
	rl.requests[email] = reqTime
	rl.mu.Unlock()
}

// Cleanup 清理过期的记录（可选的定期清理方法）
func (rl *EmailRateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	
	now := time.Now()
	for email, lastTime := range rl.requests {
		if now.Sub(lastTime) > rl.intervalTime*2 { // 保留2倍间隔时间的记录
			delete(rl.requests, email)
		}
	}
}