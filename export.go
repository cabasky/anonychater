package main

import (
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"strings"
	"time"

	"anonychater/internal/hub"
	"anonychater/internal/store"
)

func renderExportHTML(w io.Writer, msgs []store.Message, dbLabel string) error {
	ids := make([]string, 0, len(msgs))
	seen := make(map[string]bool)
	for _, m := range msgs {
		if !seen[m.SenderID] {
			seen[m.SenderID] = true
			ids = append(ids, m.SenderID)
		}
	}
	avatars := hub.AvatarMap(ids)

	var b strings.Builder

	// 头部
	b.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>anonychater 导出</title>
<style>
* { box-sizing: border-box; }
body {
  margin: 0;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
  background: #f7f7f8;
  color: #1a1a1a;
  font-size: 14px;
  padding: 24px 16px;
  -webkit-font-smoothing: antialiased;
}
.container { max-width: 900px; margin: 0 auto; }
.export-header {
  background: #fff;
  padding: 20px 24px;
  border-radius: 16px;
  margin-bottom: 20px;
  box-shadow: 0 1px 3px rgba(0,0,0,.06);
}
.export-header h1 { margin: 0 0 6px; font-size: 20px; }
.export-header .meta { color: #7a7a85; font-size: 13px; }
.export-header .meta a { color: #4d7cfe; text-decoration: none; }
.export-header .meta a:hover { text-decoration: underline; }
.messages { display: flex; flex-direction: column; }
.msg { display: flex; gap: 10px; margin-bottom: 12px; }
.msg:last-child { margin-bottom: 0; }
.msg.continuation { margin-top: -6px; }
.msg-avatar {
  width: 40px; height: 40px;
  padding: 2px;
  border-radius: 50%;
  background: #e0e0e6;
  flex-shrink: 0;
  overflow: hidden;
  align-self: flex-start;
  display: flex; align-items: center; justify-content: center;
  color: #7a7a85; font-size: 14px; font-weight: 600;
}
.msg.continuation .msg-avatar { visibility: hidden; }
.msg-avatar svg { width: 100%; height: 100%; display: block; border-radius: 50%; }
.msg-body {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  max-width: 72%;
  min-width: 0;
}
.msg-meta { margin-bottom: 4px; padding: 0 4px; }
.msg.continuation .msg-meta { display: none; }
.msg-name { font-size: 12px; font-weight: 600; color: #222222; }
.msg-time { font-size: 11px; color: #222222; opacity: .8; margin-left: 6px; }
.msg-bubble {
  padding: 9px 13px;
  border-radius: 18px;
  background: #e0e0e6;
  color: #1a1a1a;
  line-height: 1.5;
  word-break: break-word;
  white-space: pre-wrap;
}
.msg-reply-quote {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 6px 10px;
  margin-bottom: 7px;
  border-left: 3px solid #4d7cfe;
  background: rgba(0,0,0,.06);
  border-radius: 8px;
  font-size: 12px;
}
.quote-name { font-weight: 600; color: #4d7cfe; font-size: 11.5px; }
.quote-text { color: #7a7a85; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.msg-image { max-width: 240px; max-height: 240px; border-radius: 12px; display: block; }
.msg-bubble .msg-image:only-child { margin: -3px -3px; }
.msg-anchor { scroll-margin-top: 80px; }
</style>
</head>
<body>
<div class="container">
<div class="export-header">
<h1>anonychater</h1>
<div class="meta">`)

	b.WriteString(html.EscapeString(time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString(" 导出 · 共 ")
	b.WriteString(fmt.Sprintf("%d", len(msgs)))
	b.WriteString(" 条消息 · ")
	if dbLabel != "" {
		b.WriteString(html.EscapeString(dbLabel))
	} else {
		b.WriteString("当前会话")
	}
	b.WriteString(` · <a href="/exportlist">返回列表</a></div>
</div>
<div class="messages">`)

	prevSenderID := ""
	for _, m := range msgs {
		isCont := prevSenderID == m.SenderID && m.ReplyTo == ""
		writeMessage(&b, m, avatars, isCont)
		prevSenderID = m.SenderID
	}

	b.WriteString(`</div>
</div>
</body>
</html>`)

	_, err := io.WriteString(w, b.String())
	return err
}

// writeMessage 输出单条消息，**不包含任何首尾换行或空白**。
func writeMessage(b *strings.Builder, m store.Message, avatars map[string]string, isContinuation bool) {
	ts := time.UnixMilli(m.Timestamp).Format("2006-01-02 15:04")
	name := m.SenderName
	if name == "" {
		name = "匿名"
	}

	cls := "msg msg-anchor"
	if isContinuation {
		cls += " continuation"
	}

	// <div class="msg ..." id="...">
	b.WriteString(`<div class="`)
	b.WriteString(cls)
	b.WriteString(`" id="msg-`)
	b.WriteString(html.EscapeString(m.MessageID))
	b.WriteString(`">`)

	// 头像
	b.WriteString(`<div class="msg-avatar">`)
	if !isContinuation {
		if svg, ok := avatars[m.SenderID]; ok && svg != "" {
			b.WriteString(strings.TrimSpace(svg))
		} else {
			runes := []rune(name)
			b.WriteString(html.EscapeString(string(runes[0])))
		}
	}
	b.WriteString(`</div>`)

	// body
	b.WriteString(`<div class="msg-body">`)

	if !isContinuation {
		b.WriteString(`<div class="msg-meta"><span class="msg-name">`)
		b.WriteString(html.EscapeString(name))
		b.WriteString(`</span><span class="msg-time">`)
		b.WriteString(html.EscapeString(ts))
		b.WriteString(`</span></div>`)
	}

	b.WriteString(`<div class="msg-bubble">`)

	if m.ReplyTo != "" && m.ReplyToPreview != "" {
		qName := ""
		qText := m.ReplyToPreview
		if idx := strings.Index(m.ReplyToPreview, ": "); idx >= 0 {
			qName = m.ReplyToPreview[:idx]
			qText = m.ReplyToPreview[idx+2:]
		}
		b.WriteString(`<div class="msg-reply-quote"><span class="quote-name">`)
		b.WriteString(html.EscapeString(qName))
		b.WriteString(`</span><span class="quote-text">`)
		b.WriteString(html.EscapeString(qText))
		b.WriteString(`</span></div>`)
	}

	if len(m.ImageData) > 0 {
		mime := detectMime(m.ImageData)
		encoded := base64.StdEncoding.EncodeToString(m.ImageData)
		b.WriteString(`<img class="msg-image" src="data:`)
		b.WriteString(mime)
		b.WriteString(`;base64,`)
		b.WriteString(encoded)
		b.WriteString(`" alt="图片">`)
	}

	if m.Content != "" {
		b.WriteString(html.EscapeString(m.Content))
	}

	// 关掉 msg-bubble / msg-body / msg，**不输出尾部换行**
	b.WriteString(`</div></div></div>`)
}

func detectMime(data []byte) string {
	if len(data) >= 4 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "image/png"
	}
	if len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF {
		return "image/jpeg"
	}
	if len(data) >= 4 && data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "image/gif"
	}
	if len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return "image/jpeg"
}