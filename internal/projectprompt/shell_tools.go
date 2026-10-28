package projectprompt

// ShellExecExecuteGuidanceSection 供单代理/多代理系统提示追加：exec 与 execute 分工（尽量短）。
func ShellExecExecuteGuidanceSection() string {
	return `Shell (exec/execute): prefer dedicated MCP tools when one exists; use exec for system commands (pipes, workdir, background &) and execute for scripts under skills/ (paired with read_file, skill). Split multi-step scans into separate calls; never chain several scanners in one shell line. Long scripts, request bodies, or payloads must first be written into the session working directory with write_file, then run via exec/execute with a short command; never embed long content inline in command. Downloads and temporary files must go into the "session working directory" named in the system prompt, not /tmp.`
}

// ShellExecExecuteGuidanceReconSuffix 侦察子代理可选追加（一行）。
func ShellExecExecuteGuidanceReconSuffix() string {
	return `For enumeration prefer dedicated MCP tools such as subfinder or amass; avoid chaining long command lines with exec/execute.`
}
