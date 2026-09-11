package wechat

import (
	"encoding/xml"
	"regexp"
	"strconv"
	"testing"
	"time"
)

const inboundXML = `<xml><ToUserName><![CDATA[gh_test]]></ToUserName><FromUserName><![CDATA[oUser]]></FromUserName><CreateTime>1700000000</CreateTime><MsgType><![CDATA[text]]></MsgType><Content><![CDATA[第一条 #memo]]></Content><MsgId>1000000000000001</MsgId></xml>`

func TestUnmarshalInboundMessage(t *testing.T) {
	var msg InboundMessage
	if err := xml.Unmarshal([]byte(inboundXML), &msg); err != nil {
		t.Fatalf("解析入站 XML 失败: %v", err)
	}

	if msg.ToUserName != "gh_test" {
		t.Errorf("ToUserName 期望 gh_test，实际 %q", msg.ToUserName)
	}
	if msg.FromUserName != "oUser" {
		t.Errorf("FromUserName 期望 oUser，实际 %q", msg.FromUserName)
	}
	if msg.MsgType != MsgTypeText {
		t.Errorf("MsgType 期望 %q，实际 %q", MsgTypeText, msg.MsgType)
	}
	if msg.Content != "第一条 #memo" {
		t.Errorf("Content 期望 第一条 #memo，实际 %q", msg.Content)
	}
	if msg.MsgId != "1000000000000001" {
		t.Errorf("MsgId 期望 1000000000000001，实际 %q", msg.MsgId)
	}
	if msg.CreateTime != 1700000000 {
		t.Errorf("CreateTime 期望 1700000000，实际 %d", msg.CreateTime)
	}
	if msg.DedupKey() != "1000000000000001|1700000000" {
		t.Errorf("DedupKey 期望 1000000000000001|1700000000，实际 %q", msg.DedupKey())
	}
}

func TestBuildTextReplyInterchangesUsers(t *testing.T) {
	before := time.Now().Unix()
	payload := BuildTextReply("oUser", "gh_test", "✅已存入Memos")
	after := time.Now().Unix()

	// 逐字节钉住被动回复格式：XML 声明头 + 文本字段以 CDATA 承载
	pattern := `^` +
		regexp.QuoteMeta(xml.Header+`<xml>`+
			`<ToUserName><![CDATA[oUser]]></ToUserName>`+
			`<FromUserName><![CDATA[gh_test]]></FromUserName>`+
			`<CreateTime>`) +
		`(\d+)` +
		regexp.QuoteMeta(`</CreateTime>`+
			`<MsgType><![CDATA[text]]></MsgType>`+
			`<Content><![CDATA[✅已存入Memos]]></Content>`+
			`</xml>`) +
		`$`

	matched := regexp.MustCompile(pattern).FindStringSubmatch(string(payload))
	if matched == nil {
		t.Fatalf("回复 XML 不符合预期，实际: %s", string(payload))
	}

	createTime, err := strconv.ParseInt(matched[1], 10, 64)
	if err != nil {
		t.Fatalf("回复 CreateTime 无法解析: %v", err)
	}
	if createTime < before || createTime > after {
		t.Errorf("回复 CreateTime %d 应落在 [%d, %d] 之间", createTime, before, after)
	}

	// 回解一次，确认产出的确实是可被微信解析的合法 XML
	var reply InboundMessage
	if err := xml.Unmarshal(payload, &reply); err != nil {
		t.Fatalf("回复 XML 无法回解: %v", err)
	}
	if reply.Content != "✅已存入Memos" {
		t.Errorf("回复中携带的 Content 期望 ✅已存入Memos，实际 %q", reply.Content)
	}
}
