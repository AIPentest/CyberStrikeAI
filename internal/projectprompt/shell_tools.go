package projectprompt

// ShellExecExecuteGuidanceSection To be appended to the single-agent/multi-agent system prompt: the division of labor between exec and execute (as short as possible).
func ShellExecExecuteGuidanceSection() string {
	return `Shell (exec/execute): When a dedicated MCP tool is available, prefer it; for system commands (pipes, workdir, background &)use exec; for scripts under skills/ (together with read_file, skill) use execute; split multi-step scans into separate calls, and do not chain multiple scanners in one shell command. Long scripts, request bodies, or payloads must first be written to the session working directory with write_file, then run as a short command via exec/execute; do not embed long content in command. Downloads/temporary files must be written to the 'session working directory' from the system prompt, and do not use /tmp.`
}

// ShellExecExecuteGuidanceReconSuffix An optional appendix for the reconnaissance sub-agent (one line).
func ShellExecExecuteGuidanceReconSuffix() string {
	return `For enumeration, prefer dedicated MCPs such as subfinder and amass rather than chaining long exec/execute pipelines.`
}
