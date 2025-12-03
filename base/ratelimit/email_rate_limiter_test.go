package ratelimit

import (
	"testing"
	"time"
)

func TestEmailRateLimiter(t *testing.T) {
	// 创建一个1秒间隔的限流器（用于测试）
	limiter := NewEmailRateLimiter(1 * time.Second)
	testEmail := "test@example.com"

	// 第一次请求应该被允许
	canProcess, remaining := limiter.CanProcess(testEmail)
	if !canProcess {
		t.Errorf("First request should be allowed")
	}
	if remaining != 0 {
		t.Errorf("First request should have 0 remaining time")
	}

	// 立即的第二次请求应该被拒绝
	canProcess, remaining = limiter.CanProcess(testEmail)
	if canProcess {
		t.Errorf("Second immediate request should be rejected")
	}
	if remaining <= 0 {
		t.Errorf("Remaining time should be positive, got: %v", remaining)
	}

	// 等待超过限制时间后，请求应该被允许
	time.Sleep(1100 * time.Millisecond) // 稍微超过1秒
	canProcess, remaining = limiter.CanProcess(testEmail)
	if !canProcess {
		t.Errorf("Request after interval should be allowed")
	}
	if remaining != 0 {
		t.Errorf("Request after interval should have 0 remaining time")
	}
}

func TestEmailRateLimiterMultipleEmails(t *testing.T) {
	limiter := NewEmailRateLimiter(1 * time.Second)
	
	email1 := "test1@example.com"
	email2 := "test2@example.com"

	// 两个不同邮箱的请求应该互不影响
	canProcess1, _ := limiter.CanProcess(email1)
	canProcess2, _ := limiter.CanProcess(email2)
	
	if !canProcess1 || !canProcess2 {
		t.Errorf("Requests from different emails should not interfere with each other")
	}

	// 同一邮箱的第二次请求应该被拒绝
	canProcess1Again, _ := limiter.CanProcess(email1)
	if canProcess1Again {
		t.Errorf("Second request from same email should be rejected")
	}
}

func TestEmailRateLimiterCleanup(t *testing.T) {
	limiter := NewEmailRateLimiter(100 * time.Millisecond)
	
	// 记录一个请求
	limiter.CanProcess("test@example.com")
	
	// 验证记录存在
	limiter.mu.RLock()
	initialCount := len(limiter.requests)
	limiter.mu.RUnlock()
	
	if initialCount == 0 {
		t.Errorf("Should have recorded request")
	}

	// 等待足够长时间并清理
	time.Sleep(300 * time.Millisecond)
	limiter.Cleanup()
	
	// 验证记录被清理
	limiter.mu.RLock()
	afterCleanupCount := len(limiter.requests)
	limiter.mu.RUnlock()
	
	if afterCleanupCount > 0 {
		t.Errorf("Cleanup should have removed expired records")
	}
}