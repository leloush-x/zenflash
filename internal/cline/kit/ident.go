package kit

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func WithRetryJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}
	// 在退避时间上增加 0%~25% 抖动，避免多个请求同时重试。
	jitter := time.Duration(float64(delay) * float64(RandIntn(26)) / 100)
	const maxDuration = time.Duration(1<<63 - 1)
	if delay > maxDuration-jitter {
		return maxDuration
	}
	return delay + jitter
}

// ============================================================================
// 客户端身份轮换: 模拟多个独立 opencode 客户端
// 源码中 x-opencode-session / x-opencode-request / User-Agent 均为客户端随机生成,
// 服务端可能按这些标识记账(实测 "Worker local total request limit reached" 疑似 session 级)。
// 每次请求生成全新身份 = 每次都是"新客户端",从源头规避身份维度的限流。
// ============================================================================

var ZenUserAgents = []string{
	"opencode/latest/1.18.14/cli",
	"opencode/latest/1.18.13/cli",
	"opencode/1.18.14/cli",
	"opencode/1.18.13/cli",
	"opencode/1.18.12/cli",
	"opencode/1.18.11/cli",
	"opencode/latest/1.18.14/desktop",
	"opencode/latest/1.18.13/desktop",
}

func RandHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func RandIntn(n int) int {
	if n <= 0 {
		return 0
	}
	b := make([]byte, 4)
	rand.Read(b)
	v := int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3])
	if v < 0 {
		v = -v
	}
	return v % n
}

// zenBase62 is the alphabet for the random tail of a canonical session id.
const zenBase62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// canonicalZenSession builds a random session id in OpenCode's canonical
// shape ("ses_" + 12 lowercase hex + 14 Base62). The Zen free tier rejects
// any other session shape with 403, so rotation must stay canonical: every
// call still returns a fresh identity, only the format is fixed.
func canonicalZenSession() string {
	timePart := RandHex(6)
	tail := make([]byte, 14)
	for i := range tail {
		tail[i] = zenBase62[RandIntn(len(zenBase62))]
	}
	return "ses_" + timePart + string(tail)
}

// FreshZenIdentity 生成一组全新客户端身份 (session, request, user-agent)
func FreshZenIdentity() (string, string, string) {
	return canonicalZenSession(),
		"user_" + RandHex(8),
		ZenUserAgents[RandIntn(len(ZenUserAgents))]
}
