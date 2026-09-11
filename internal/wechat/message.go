package wechat

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"time"
)

// MsgTypeText 是唯一会被处理的消息类型，其余类型一律丢弃。
const MsgTypeText = "text"

// InboundMessage 只声明本项目关心的字段，图片、语音等字段直接不解析。
type InboundMessage struct {
	XMLName      xml.Name `xml:"xml"`
	ToUserName   string   `xml:"ToUserName"`
	FromUserName string   `xml:"FromUserName"`
	CreateTime   int64    `xml:"CreateTime"`
	MsgType      string   `xml:"MsgType"`
	Content      string   `xml:"Content"`
	// MsgId 用字符串承载，避免超出 int64 范围后解析失败。
	MsgId string `xml:"MsgId"`
}

// DedupKey 以 MsgId+CreateTime 标识一次推送：微信超时重试会复用同一组值。
func (m InboundMessage) DedupKey() string {
	return m.MsgId + "|" + strconv.FormatInt(m.CreateTime, 10)
}

// replyTemplate 是微信被动回复的 XML 格式。
// encoding/xml 无法在具名字段上输出 CDATA（,cdata 会退化为 chardata 而丢掉元素名），
// 因此这里直接按微信文档的形态拼装。
const replyTemplate = `<xml>` +
	`<ToUserName><![CDATA[%s]]></ToUserName>` +
	`<FromUserName><![CDATA[%s]]></FromUserName>` +
	`<CreateTime>%d</CreateTime>` +
	`<MsgType><![CDATA[` + MsgTypeText + `]]></MsgType>` +
	`<Content><![CDATA[%s]]></Content>` +
	`</xml>`

// BuildTextReply 构造文本回复：收件人与发件人相对入站消息互换。
func BuildTextReply(toUser, fromUser, content string) []byte {
	payload := fmt.Sprintf(replyTemplate, toUser, fromUser, time.Now().Unix(), content)
	return []byte(xml.Header + payload)
}
