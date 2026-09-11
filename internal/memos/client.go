// Package memos 封装对自建 Memos 的写入调用。
package memos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Timeout 约束单次 Memos 调用的最长耗时，回调侧复用同一常量。
const Timeout = 10 * time.Second

// createMemoPath 对应 Memos v0.22+ 的 CreateMemo 接口（复数路径，单数路径仅存在于旧版）。
const createMemoPath = "/api/v1/memos"

// visibilityPrivate 备忘录固定为私有，不接受外部传入。
const visibilityPrivate = "PRIVATE"

// maxErrBodyBytes 限制错误响应体的读取量，避免被超大响应拖住。
const maxErrBodyBytes = 512

// Client 只做一件事：往 Memos 写一条私有备忘录。
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// New 创建 Memos 客户端，baseURL 为实例基地址（如 https://memos.example.com）。
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		http: &http.Client{
			Timeout: Timeout,
		},
	}
}

// createMemoRequest 只声明需求要求的字段，不做额外字段拼装。
type createMemoRequest struct {
	Content    string `json:"content"`
	Visibility string `json:"visibility"`
}

// CreatePrivateMemo 以 PRIVATE 可见性创建一条备忘录，content 原样透传。
func (c *Client) CreatePrivateMemo(ctx context.Context, content string) error {
	payload, err := json.Marshal(createMemoRequest{
		Content:    content,
		Visibility: visibilityPrivate,
	})
	if err != nil {
		return fmt.Errorf("序列化 memo 请求体失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+createMemoPath, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("构造 memo 请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("请求 Memos 失败: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBodyBytes))
		return fmt.Errorf("Memos 返回非 2xx: status=%d body=%s", resp.StatusCode, string(body))
	}

	return nil
}
