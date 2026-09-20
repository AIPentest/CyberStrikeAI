package openai

// eino_sse_sanitizer.go 解决 Eino 走 meguminnnnnnnnn/go-openai SDK 时，
// 中转站心跳/SSE 控制行累计 > 300 行触发 ErrTooManyEmptyStreamMessages
// （报错文案: "stream has sent too many empty messages"）的问题。
//
// 触发链路:
//   einoopenai.NewChatModel
//     → eino-ext/libs/acl/openai → meguminnnnnnnnn/go-openai
//     → streamReader.processLines() 对所有非 "data:" 行计数, > 300 即抛错。
//
// 中转站常见的非 data: 行（合法 SSE 但 SDK 不接受）:
//   ":" / ": keepalive" / ": ping" / "event: ping" / "retry: 3000"
//   以及思考型模型 prefill 期间穿插的大量心跳。
//
// 兜底策略: 在 HTTP transport 层把响应 Body 包一层 reader, 只放行 "data:"
// 开头的行, 把心跳/注释/事件类型行就地吞掉。下游 SDK 永远见不到非 data: 行,
// 计数器始终为 0, 该错误不可能再发生。
//
// 该层对调用方完全透明:
//   - 仅当响应 Content-Type 是 text/event-stream 时介入；普通 JSON 响应原样透传
//   - data: payload (含 [DONE] 与 {"error":...}) 一字节不改
//   - 上游真断流 (EOF / connection reset / context cancel) 原样透传

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"strings"
)

const (
	// einoSSEReaderBufSize 给 bufio 一个较大的初始缓冲, 避免单行大 JSON chunk
	// (含工具调用 arguments / reasoning_content) 频繁触发缓冲区扩容。
	einoSSEReaderBufSize = 64 * 1024
)

// einoSSESanitizingRoundTripper 包装下游 RoundTripper, 对 SSE 响应做行级清洗。
type einoSSESanitizingRoundTripper struct {
	base http.RoundTripper
}

func (rt *einoSSESanitizingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := rt.base.RoundTrip(req)
	if err != nil || resp == nil {
		return resp, err
	}
	if !isSSEResponse(resp) {
		return resp, nil
	}
	resp.Body = newEinoSSESanitizingBody(resp.Body)
	return resp, nil
}

// isSSEResponse 仅对 200 + text/event-stream 的响应做清洗;
// 错误响应由独立的 einoSSEErrorRoundTripper 处理，此层不改动。
func isSSEResponse(resp *http.Response) bool {
	if resp.StatusCode != http.StatusOK {
		return false
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		return false
	}
	ct = strings.ToLower(strings.TrimSpace(ct))
	// 兼容 "text/event-stream", "text/event-stream; charset=utf-8" 等。
	return strings.HasPrefix(ct, "text/event-stream")
}

// einoSSESanitizingBody 是包装后的响应体: 只放行 data: 行, 其它行吞掉。
type einoSSESanitizingBody struct {
	upstream io.ReadCloser
	reader   *bufio.Reader
	pending  []byte // 已清洗、待返回给下游的字节 (永远以 \n 结尾的完整 data: 行)
	err      error  // upstream 终态错误 (io.EOF 或网络错误)
}

func newEinoSSESanitizingBody(body io.ReadCloser) *einoSSESanitizingBody {
	return &einoSSESanitizingBody{
		upstream: body,
		reader:   bufio.NewReaderSize(body, einoSSEReaderBufSize),
	}
}

func (b *einoSSESanitizingBody) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}

	// 从上游读, 直到攒出一行 data: 或拿到终态。
	// 单次循环可能丢弃任意多行心跳, 但只放行至多一行 data: 后退出,
	// 避免一次 Read 阻塞过久 / pending 缓冲过大。
	for b.err == nil {
		line, err := b.reader.ReadBytes('\n')
		if len(line) > 0 {
			if isPassThroughSSELine(line) {
				if line[len(line)-1] != '\n' {
					line = append(line, '\n')
				}
				b.pending = line
				if err != nil {
					b.err = err
				}
				break
			}
			// 非 data: 行 (空行 / ":" 注释 / event: / retry: / id: / 任何裸文本)
			// 全部吞掉, 不向下游透出, 继续循环读下一行。
		}
		if err != nil {
			b.err = err
			break
		}
	}

	if len(b.pending) > 0 {
		n := copy(p, b.pending)
		b.pending = b.pending[n:]
		return n, nil
	}
	return 0, b.err
}

func (b *einoSSESanitizingBody) Close() error {
	return b.upstream.Close()
}

// isPassThroughSSELine 判定该行是否需要原样放行给下游 SDK。
// 仅 "data:" (大小写不敏感, 可有任意前导空白) 开头的行需要保留。
// 注意: 不能用 TrimSpace 去尾部换行后再判, 否则 "  data: x" 会被误判;
// 我们只 trim 前导空白, 与 SDK 内部 TrimSpace 后再正则 ^data:\s* 的语义一致。
func isPassThroughSSELine(line []byte) bool {
	trimmed := bytes.TrimLeft(line, " \t")
	if len(trimmed) < 5 {
		return false
	}
	// 大小写不敏感比较前 5 字节是否为 "data:"。SSE 规范要求字段名小写,
	// 但宽松匹配可以兼容个别中转站的非规范实现。
	return (trimmed[0] == 'd' || trimmed[0] == 'D') &&
		(trimmed[1] == 'a' || trimmed[1] == 'A') &&
		(trimmed[2] == 't' || trimmed[2] == 'T') &&
		(trimmed[3] == 'a' || trimmed[3] == 'A') &&
		trimmed[4] == ':'
}
