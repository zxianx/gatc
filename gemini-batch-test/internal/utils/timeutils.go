package utils

import (
	"fmt"
	"time"
)

// FormatDuration 格式化时长为人类可读的字符串
func FormatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	} else {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
}

// FormatTime 格式化时间为标准字符串
func FormatTime(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// TimeSince 计算从指定时间到现在的时长
func TimeSince(t time.Time) time.Duration {
	return time.Since(t)
}

// CalculateProgress 计算进度百分比
func CalculateProgress(completed, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(completed) / float64(total) * 100
}