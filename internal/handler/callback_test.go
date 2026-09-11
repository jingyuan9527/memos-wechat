package handler

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"memos-wechat/internal/config"
	"memos-wechat/internal/dedupe"
	"memos-wechat/internal/memos"
)

const (
	testToken     = "testtoken"
	testTimestamp = "1700000000"
	testNonce     = "abc123xyz"
	testOpenID    = "oALLOWED"
	testMemosURL  = "http://127.0.0.1:1"
)

// memosCall 记录一次到达 Memos 的请求，用于断言"到底写了几条、写了什么"。
type memosCall struct {
	method string
	path   string
	auth   string
	body   string
}

type mockMemos struct {
	server *httptest.Server
	status int

	mu    sync.Mutex
	calls []memosCall
}

func newMockMemos(t *testing.T, status int) *mockMemos {
	t.Helper()

	mock := &mockMemos{status: status}
	mock.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		mock.mu.Lock()
		mock.calls = append(mock.calls, memosCall{
			method: r.Method,
			path:   r.URL.Path,
			auth:   r.Header.Get("Authorization"),
			body:   string(body),
		})
		mock.mu.Unlock()

		w.WriteHeader(mock.status)
	}))
	t.Cleanup(mock.server.Close)

	return mock
}

func (m *mockMemos) recorded() []memosCall {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]memosCall(nil), m.calls...)
}

func newTestCallback(t *testing.T, memosURL string) *Callback {
	t.Helper()

	cfg := &config.Config{
		WechatToken:       testToken,
		WechatAllowOpenID: testOpenID,
		MemosAPIURL:       memosURL,
		MemosAccessToken:  "test-memos-token",
		ListenPort:        "0",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	return NewCallback(cfg, memos.New(memosURL, cfg.MemosAccessToken), dedupe.NewCache(time.Minute), logger)
}

// assertTextReply 校验回复是标准的文本被动回复，而非占位响应。
// 仅做子串匹配会放过结构错误的 XML，故这里按字段逐个断言。
func assertTextReply(t *testing.T, body string) {
	t.Helper()

	var reply struct {
		ToUserName   string `xml:"ToUserName"`
		FromUserName string `xml:"FromUserName"`
		CreateTime   int64  `xml:"CreateTime"`
		MsgType      string `xml:"MsgType"`
		Content      string `xml:"Content"`
	}
	if err := xml.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatalf("回复不是合法 XML: %v, body=%q", err, body)
	}

	if reply.MsgType != "text" {
		t.Errorf("回复 MsgType 期望 text，实际 %q（body=%q）", reply.MsgType, body)
	}
	if reply.Content != replyMemoStored {
		t.Errorf("回复 Content 期望 %q，实际 %q", replyMemoStored, reply.Content)
	}
	if reply.ToUserName != testOpenID {
		t.Errorf("回复 ToUserName 期望 %q，实际 %q", testOpenID, reply.ToUserName)
	}
	if reply.FromUserName != "gh_test" {
		t.Errorf("回复 FromUserName 期望 gh_test，实际 %q", reply.FromUserName)
	}
	if reply.CreateTime <= 0 {
		t.Errorf("回复 CreateTime 应为正数，实际 %d", reply.CreateTime)
	}
	// 微信要求文本字段以 CDATA 承载
	if !strings.Contains(body, "<![CDATA["+replyMemoStored+"]]>") {
		t.Errorf("回复内容应以 CDATA 承载，实际 %q", body)
	}
}

// sign 独立复现一份签名算法，避免用被测代码生成测试输入。
func sign() string {
	parts := []string{testToken, testTimestamp, testNonce}
	sort.Strings(parts)

	sum := sha1.Sum([]byte(strings.Join(parts, "")))
	return hex.EncodeToString(sum[:])
}

func callbackURL(signature string) string {
	return fmt.Sprintf("/callback?signature=%s&timestamp=%s&nonce=%s", signature, testTimestamp, testNonce)
}

func inboundXML(fromUser, msgType, content, msgID string) string {
	return fmt.Sprintf(
		`<xml><ToUserName><![CDATA[gh_test]]></ToUserName><FromUserName><![CDATA[%s]]></FromUserName>`+
			`<CreateTime>%s</CreateTime><MsgType><![CDATA[%s]]></MsgType>`+
			`<Content><![CDATA[%s]]></Content><MsgId>%s</MsgId></xml>`,
		fromUser, testTimestamp, msgType, content, msgID,
	)
}

// postMessage 直接驱动 handler，并在返回前等完异步写入，保证断言时结果已确定。
func postMessage(t *testing.T, callback *Callback, fromUser, msgType, content, msgID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, callbackURL(sign()), strings.NewReader(inboundXML(fromUser, msgType, content, msgID)))
	rec := httptest.NewRecorder()
	callback.dispatch(rec, req)
	callback.Wait()

	return rec
}

func TestWhitelistedTextCreatesOnePrivateMemo(t *testing.T) {
	mock := newMockMemos(t, http.StatusOK)
	callback := newTestCallback(t, mock.server.URL)

	rec := postMessage(t, callback, testOpenID, "text", "第一条 #memo", "1000000000000001")

	if rec.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", rec.Code)
	}
	assertTextReply(t, rec.Body.String())

	calls := mock.recorded()
	if len(calls) != 1 {
		t.Fatalf("期望写入 1 条 memo，实际 %d 条: %+v", len(calls), calls)
	}

	want := memosCall{
		method: http.MethodPost,
		path:   "/api/v1/memos",
		auth:   "Bearer test-memos-token",
		body:   `{"content":"第一条 #memo","visibility":"PRIVATE"}`,
	}
	if calls[0] != want {
		t.Fatalf("Memos 请求不符合预期\nwant: %+v\ngot:  %+v", want, calls[0])
	}
}

func TestDuplicatePushCreatesOnlyOneMemo(t *testing.T) {
	mock := newMockMemos(t, http.StatusOK)
	callback := newTestCallback(t, mock.server.URL)

	// 模拟微信超时重试：同 MsgId+CreateTime 连推三次
	for i := 0; i < 3; i++ {
		rec := postMessage(t, callback, testOpenID, "text", "重复消息", "1000000000000001")
		assertTextReply(t, rec.Body.String())
	}

	if got := len(mock.recorded()); got != 1 {
		t.Fatalf("重复推送应只创建 1 条 memo，实际 %d 条", got)
	}
}

func TestDistinctMsgIDCreatesSeparateMemo(t *testing.T) {
	mock := newMockMemos(t, http.StatusOK)
	callback := newTestCallback(t, mock.server.URL)

	postMessage(t, callback, testOpenID, "text", "消息一", "1000000000000001")
	postMessage(t, callback, testOpenID, "text", "消息二", "1000000000000002")

	if got := len(mock.recorded()); got != 2 {
		t.Fatalf("不同 MsgId 应各自创建 memo，实际 %d 条", got)
	}
}

func TestNonWhitelistedOpenIDIsIgnored(t *testing.T) {
	mock := newMockMemos(t, http.StatusOK)
	callback := newTestCallback(t, mock.server.URL)

	rec := postMessage(t, callback, "oOTHER", "text", "不该被写入", "1000000000000001")

	if rec.Body.String() != ignoreReplyBody {
		t.Fatalf("非白名单应返回 %q，实际 %q", ignoreReplyBody, rec.Body.String())
	}
	if got := len(mock.recorded()); got != 0 {
		t.Fatalf("非白名单不应写入 memo，实际 %d 条", got)
	}
}

func TestNonTextMessageIsIgnored(t *testing.T) {
	mock := newMockMemos(t, http.StatusOK)
	callback := newTestCallback(t, mock.server.URL)

	rec := postMessage(t, callback, testOpenID, "image", "", "1000000000000001")

	if rec.Body.String() != ignoreReplyBody {
		t.Fatalf("图片消息应返回 %q，实际 %q", ignoreReplyBody, rec.Body.String())
	}
	if got := len(mock.recorded()); got != 0 {
		t.Fatalf("图片消息不应写入 memo，实际 %d 条", got)
	}
}

func TestMemosFailureStillReplies(t *testing.T) {
	mock := newMockMemos(t, http.StatusInternalServerError)
	callback := newTestCallback(t, mock.server.URL)

	rec := postMessage(t, callback, testOpenID, "text", "下游挂掉也要有回复", "1000000000000001")

	if rec.Code != http.StatusOK {
		t.Fatalf("Memos 失败时回调仍应返回 200，实际 %d", rec.Code)
	}
	assertTextReply(t, rec.Body.String())

	if got := len(mock.recorded()); got != 1 {
		t.Fatalf("应尝试调用一次 Memos，实际 %d 次", got)
	}
}

func TestMemosUnreachableStillReplies(t *testing.T) {
	callback := newTestCallback(t, testMemosURL)

	rec := postMessage(t, callback, testOpenID, "text", "Memos 服务不可用", "1000000000000001")

	if rec.Code != http.StatusOK {
		t.Fatalf("Memos 不可用时应回调仍返回 200，实际 %d", rec.Code)
	}
	assertTextReply(t, rec.Body.String())
}

func TestPostWithInvalidSignatureIsRejected(t *testing.T) {
	mock := newMockMemos(t, http.StatusOK)
	callback := newTestCallback(t, mock.server.URL)

	req := httptest.NewRequest(http.MethodPost, callbackURL("deadbeef"), strings.NewReader(inboundXML(testOpenID, "text", "伪造请求", "1000000000000001")))
	rec := httptest.NewRecorder()
	callback.dispatch(rec, req)
	callback.Wait()

	if rec.Code != http.StatusForbidden {
		t.Fatalf("签名错误应返回 403，实际 %d", rec.Code)
	}
	if got := len(mock.recorded()); got != 0 {
		t.Fatalf("签名错误不应写入 memo，实际 %d 条", got)
	}
}

func TestVerifyURLEchoesEchostr(t *testing.T) {
	callback := newTestCallback(t, testMemosURL)

	pass := httptest.NewRecorder()
	callback.dispatch(pass, httptest.NewRequest(http.MethodGet, callbackURL(sign())+"&echostr=echo-ok", nil))
	if pass.Body.String() != "echo-ok" {
		t.Fatalf("校验通过应回显 echostr，实际 %q", pass.Body.String())
	}

	fail := httptest.NewRecorder()
	callback.dispatch(fail, httptest.NewRequest(http.MethodGet, callbackURL("deadbeef")+"&echostr=echo-ok", nil))
	if fail.Body.String() != "fail" {
		t.Fatalf("校验失败应返回 fail，实际 %q", fail.Body.String())
	}
}

func TestUnsupportedMethodIsRejected(t *testing.T) {
	callback := newTestCallback(t, testMemosURL)

	rec := httptest.NewRecorder()
	callback.dispatch(rec, httptest.NewRequest(http.MethodDelete, callbackURL(sign()), nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("非 GET/POST 应返回 405，实际 %d", rec.Code)
	}
}

func TestOnlyCallbackRouteIsExposed(t *testing.T) {
	callback := newTestCallback(t, testMemosURL)
	mux := http.NewServeMux()
	callback.Register(mux)

	ok := httptest.NewRecorder()
	mux.ServeHTTP(ok, httptest.NewRequest(http.MethodGet, callbackURL(sign())+"&echostr=echo-ok", nil))
	if ok.Code != http.StatusOK {
		t.Fatalf("/callback 应可访问，实际 %d", ok.Code)
	}

	for _, path := range []string{"/", "/admin", "/callback/extra"} {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s 应返回 404，实际 %d", path, rec.Code)
		}
	}
}
