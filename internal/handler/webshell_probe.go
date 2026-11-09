package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

// webshellOSProbeCommand 探活命令：利用 Windows cmd 与 POSIX shell 对 `%OS%` 展开差异进行判定。
//   - Windows cmd：`%OS%` 被展开为 `Windows_NT`，回显 `:OSPROBE_Windows_NT:END`
//   - POSIX sh/bash：`%OS%` 不是变量语法，作为字面量原样保留，回显 `:OSPROBE_%OS%:END`
//
// 一条命令即可得到明确的、互斥的信号，避免探活成本（相比发两次命令）。
// 冒号包裹是为了避免部分 shell 输出多余空白/BOM 时字符串匹配失效。
const webshellOSProbeCommand = "echo :OSPROBE_%OS%:END"

// probeWebshellOSViaExec 通过一次命令执行的回显推断目标操作系统。
//
// 返回值：
//   - "windows" / "linux"：识别成功
//   - ""：无法判定（调用方应保留既有 fallback 逻辑）
//
// 入参 execFn 是一个"发命令并拿到回显"的闭包；让 HTTP 入口和 MCP 入口可以共用同一套探活逻辑
// 而不必关心底层是如何发包的。
func probeWebshellOSViaExec(execFn func(cmd string) (output string, ok bool)) string {
	if execFn == nil {
		return ""
	}
	out, ok := execFn(webshellOSProbeCommand)
	if !ok {
		return ""
	}
	return classifyWebshellOSProbeOutput(out)
}

// classifyWebshellOSProbeOutput 纯函数：根据探活命令的回显判定 OS。
// 抽出来是为了单测可直接覆盖所有分支，无需真实 HTTP 调用。
func classifyWebshellOSProbeOutput(out string) string {
	if out == "" {
		return ""
	}
	lower := strings.ToLower(out)

	// Windows 强信号：cmd.exe 成功展开了 %OS% 变量
	if strings.Contains(out, "Windows_NT") {
		return "windows"
	}
	// 容错：部分老版本 Windows 可能 `%OS%` 展开为其他字样（极少见），再看 PATH/OS 等次级线索
	if strings.Contains(lower, "microsoft windows") {
		return "windows"
	}

	// Linux/Unix 强信号：`%OS%` 字面量被原样回显，说明 shell 不是 cmd.exe
	if strings.Contains(out, "%OS%") {
		return "linux"
	}

	// 次级线索：部分 webshell 在 Linux 上可能走了其他外壳（如 zsh/ash），
	// 但它们对 `%OS%` 同样不展开；若命中 OSPROBE 头部却没拿到 %OS% 字面量，
	// 说明回显被中途截断或过滤，保守返回空让上层 fallback。
	return ""
}

// newHTTPExecFn 为 HTTP FileOp 路径构造"发命令取回显"的闭包，供探活复用。
// 参数来自 HTTP 请求，复用 buildExecURL / buildExecBody 两个已有的命令编排器，
// 确保探活包与实际文件操作包走完全一致的 webshell 协议（GET/POST、参数名、编码）。
func (h *WebShellHandler) newHTTPExecFn(targetURL, password, shellType, method, cmdParam, encoding string) func(string) (string, bool) {
	useGET := strings.ToUpper(strings.TrimSpace(method)) == "GET"
	if strings.TrimSpace(cmdParam) == "" {
		cmdParam = "cmd"
	}
	return func(cmd string) (string, bool) {
		var (
			httpReq *http.Request
			err     error
		)
		if useGET {
			u := h.buildExecURL(targetURL, shellType, password, cmdParam, cmd)
			httpReq, err = http.NewRequest(http.MethodGet, u, nil)
		} else {
			body := h.buildExecBody(shellType, password, cmdParam, cmd)
			httpReq, err = http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(body))
			if err == nil {
				httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
		}
		if err != nil {
			return "", false
		}
		httpReq.Header.Set("User-Agent", "Mozilla/5.0 (compatible; CyberStrikeAI-WebShell/1.0)")
		resp, err := h.client.Do(httpReq)
		if err != nil {
			return "", false
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return decodeWebshellOutput(raw, encoding), resp.StatusCode == http.StatusOK
	}
}

// persistDetectedOS 把探活结果回写到连接表；失败只记日志不阻断主流程。
// 设计上故意只触发 UPDATE，不会新建记录，因此即便 connectionID 不存在也只是悄悄放弃。
func (h *WebShellHandler) persistDetectedOS(connectionID, detected string) {
	connectionID = strings.TrimSpace(connectionID)
	detected = normalizeWebshellOS(detected)
	if connectionID == "" || detected == "" || detected == "auto" {
		return
	}
	conn, err := h.db.GetWebshellConnection(connectionID)
	if err != nil || conn == nil {
		// 不是所有调用方都能提供有效 ID（比如临时测试），这里静默返回
		return
	}
	if normalizeWebshellOS(conn.OS) != "auto" {
		// 用户已经显式选过 OS，尊重用户选择，不自动覆盖
		return
	}
	conn.OS = detected
	if err := h.db.UpdateWebshellConnection(conn); err != nil {
		h.logger.Warn("failed to persist webshell probe result", zap.String("id", connectionID), zap.String("os", detected), zap.Error(err))
		return
	}
	h.logger.Info("webshell auto OS probe succeeded and persisted", zap.String("id", connectionID), zap.String("os", detected))
}
