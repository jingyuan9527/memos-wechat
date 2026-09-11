// Package wechat 承载微信公众号回调协议：签名校验与消息 XML 编解码。
package wechat

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
	"sort"
	"strings"
)

// CheckSignature 按微信规则校验签名：token、timestamp、nonce 字典序拼接后取 sha1。
func CheckSignature(token, signature, timestamp, nonce string) bool {
	if token == "" || signature == "" {
		return false
	}

	parts := []string{token, timestamp, nonce}
	sort.Strings(parts)

	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	expected := hex.EncodeToString(sum[:])

	// 恒定时间比较，避免签名校验被计时侧信道探测。
	return subtle.ConstantTimeCompare([]byte(expected), []byte(signature)) == 1
}
