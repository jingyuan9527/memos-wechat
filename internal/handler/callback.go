// Package handler 编排 /callback 回调：校验、过滤、去重、异步落库。
package handler

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"

	"memos-wechat/internal/config"
	"memos-wechat/internal/dedupe"
	"memos-wechat/internal/memos"
	"memos-wechat/internal/wechat"
)

const (
	// 微信要求 5s 内响应；请求体只可能是小体积 XML，设上限防滥用。
	maxBodyBytes = 64 << 10

	// ignoreReplyBody 是微信约定的"不回复"占位响应，可阻止其重试。
	ignoreReplyBody = "success"

	// replyMemoStored 是需求指定的固定回复文案。
	replyMemoStored = "✅已存入Memos"

	contentTypePlain = "text/plain; charset=utf-8"
	contentTypeXML   = "text/xml; charset=utf-8"
)

// Callback 处理微信公众号的全部回调流量。
type Callback struct {
	cfg    *config.Config
	memos  *memos.Client
	dedupe *dedupe.Cache
	logger *slog.Logger

	// wg 跟踪已派发的异步写入，供进程退出时等待在途请求。
	wg sync.WaitGroup
}

// NewCallback 组装回调处理器。
func NewCallback(cfg *config.Config, memosClient *memos.Client, cache *dedupe.Cache, logger *slog.Logger) *Callback {
	return &Callback{
		cfg:    cfg,
		memos:  memosClient,
		dedupe: cache,
		logger: logger,
	}
}

// Register 只挂载 /callback；其余路径由 ServeMux 兜底为 404。
func (h *Callback) Register(mux *http.ServeMux) {
	mux.HandleFunc("/callback", h.dispatch)
}

// Wait 阻塞至所有在途的异步 Memos 调用结束，避免退出时丢消息。
func (h *Callback) Wait() {
	h.wg.Wait()
}

func (h *Callback) dispatch(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.verifyURL(w, r)
	case http.MethodPost:
		h.receiveMessage(w, r)
	default:
		h.logger.Warn("拒绝非 GET/POST 请求",
			"method", r.Method,
			"path", r.URL.Path,
			"remote", r.RemoteAddr,
		)
		w.Header().Set("Allow", "GET, POST")
		writePlain(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// verifyURL 处理微信后台配置回调地址时的签名校验，通过则原样回显 echostr。
func (h *Callback) verifyURL(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	signature := query.Get("signature")
	timestamp := query.Get("timestamp")
	nonce := query.Get("nonce")

	if !wechat.CheckSignature(h.cfg.WechatToken, signature, timestamp, nonce) {
		h.logger.Warn("GET 签名校验失败",
			"remote", r.RemoteAddr,
			"signature", signature,
			"timestamp", timestamp,
			"nonce", nonce,
		)
		writePlain(w, http.StatusOK, "fail")
		return
	}

	h.logger.Info("GET 签名校验通过", "remote", r.RemoteAddr, "timestamp", timestamp)
	writePlain(w, http.StatusOK, query.Get("echostr"))
}

// receiveMessage 处理消息推送，全程不阻塞在 Memos 调用上。
func (h *Callback) receiveMessage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if !wechat.CheckSignature(h.cfg.WechatToken, query.Get("signature"), query.Get("timestamp"), query.Get("nonce")) {
		h.logger.Warn("POST 签名校验失败，丢弃推送",
			"remote", r.RemoteAddr,
			"signature", query.Get("signature"),
			"timestamp", query.Get("timestamp"),
			"nonce", query.Get("nonce"),
		)
		writePlain(w, http.StatusForbidden, "fail")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		h.logger.Error("读取推送请求体失败", "remote", r.RemoteAddr, "error", err)
		writePlain(w, http.StatusOK, ignoreReplyBody)
		return
	}

	var msg wechat.InboundMessage
	if err := xml.Unmarshal(body, &msg); err != nil {
		h.logger.Error("解析推送 XML 失败", "remote", r.RemoteAddr, "raw", string(body), "error", err)
		writePlain(w, http.StatusOK, ignoreReplyBody)
		return
	}

	h.logger.Info("收到微信推送",
		"openid", msg.FromUserName,
		"msgType", msg.MsgType,
		"msgId", msg.MsgId,
		"createTime", msg.CreateTime,
		"content", msg.Content,
	)

	if msg.FromUserName != h.cfg.WechatAllowOpenID {
		h.logger.Warn("发送者不在白名单，忽略",
			"openid", msg.FromUserName,
			"msgId", msg.MsgId,
		)
		writePlain(w, http.StatusOK, ignoreReplyBody)
		return
	}

	if msg.MsgType != wechat.MsgTypeText {
		h.logger.Info("非文本消息，忽略",
			"openid", msg.FromUserName,
			"msgType", msg.MsgType,
			"msgId", msg.MsgId,
		)
		writePlain(w, http.StatusOK, ignoreReplyBody)
		return
	}

	// 去重判定必须先于异步派发完成，否则并发的微信重试会同时漏过检查。
	if h.dedupe.MarkSeen(msg.DedupKey()) {
		h.logger.Info("重复推送，跳过 Memos 调用",
			"openid", msg.FromUserName,
			"msgId", msg.MsgId,
			"createTime", msg.CreateTime,
		)
		writeTextReply(w, &msg, replyMemoStored)
		return
	}

	h.enqueueMemo(msg.Content, msg.FromUserName, msg.MsgId)

	writeTextReply(w, &msg, replyMemoStored)
}

// enqueueMemo 把 Memos 调用移出回调链路：微信只看回调响应，不感知下游结果。
func (h *Callback) enqueueMemo(content, openid, msgID string) {
	h.wg.Add(1)

	go func() {
		defer h.wg.Done()

		// 单条消息的异常不能拖垮整个服务进程。
		defer func() {
			if recovered := recover(); recovered != nil {
				h.logger.Error("异步写入 Memos 时发生 panic",
					"openid", openid,
					"msgId", msgID,
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), memos.Timeout)
		defer cancel()

		if err := h.memos.CreatePrivateMemo(ctx, content); err != nil {
			h.logger.Error("写入 Memos 失败",
				"openid", openid,
				"msgId", msgID,
				"contentLength", len(content),
				"error", err,
			)
			return
		}

		h.logger.Info("已写入 Memos",
			"openid", openid,
			"msgId", msgID,
			"contentLength", len(content),
		)
	}()
}

// writeTextReply 输出固定文案的被动回复。
func writeTextReply(w http.ResponseWriter, msg *wechat.InboundMessage, content string) {
	w.Header().Set("Content-Type", contentTypeXML)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(wechat.BuildTextReply(msg.FromUserName, msg.ToUserName, content))
}

func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", contentTypePlain)
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
