package security

import "strings"

const backgroundJobStdioRedirect = " </dev/null >/dev/null 2>&1"

// findStandaloneAmpersandPositions Return standalone ones not inside quotes & index (excluding &&).
func findStandaloneAmpersandPositions(command string) []int {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	var positions []int
	inSingleQuote := false
	inDoubleQuote := false
	escaped := false

	for i := 0; i < len(command); i++ {
		r := command[i]
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}
		if r == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}
		if r != '&' || inSingleQuote || inDoubleQuote {
			continue
		}
		if i+1 < len(command) && command[i+1] == '&' {
			continue
		}
		if i > 0 && command[i-1] == '&' {
			continue
		}

		isStandalone := i == 0
		if !isStandalone {
			prev := command[i-1]
			isStandalone = prev == ' ' || prev == '\t' || prev == '\n' || prev == '\r'
		}
		if !isStandalone {
			continue
		}
		if i == len(command)-1 {
			positions = append(positions, i)
			continue
		}
		next := command[i+1]
		if next == ' ' || next == '\t' || next == '\n' || next == '\r' {
			positions = append(positions, i)
		}
	}
	return positions
}

func segmentHasStdioRedirect(segment string) bool {
	lower := strings.ToLower(strings.TrimSpace(segment))
	if lower == "" {
		return false
	}
	if strings.Contains(lower, ">/dev/null") || strings.Contains(lower, "2>/dev/null") {
		return true
	}
	if strings.Contains(lower, "&>") || strings.Contains(lower, "&>>") {
		return true
	}
	if strings.Contains(lower, "2>&1") && strings.Contains(lower, "/dev/null") {
		return true
	}
	return false
}

// RedirectBackgroundJobStdio for each standalone & background segment injection before </dev/null >/dev/null 2>&1,
// Avoid background child processes holding the execute/exec pipe and causing a hang.
func RedirectBackgroundJobStdio(command string) string {
	positions := findStandaloneAmpersandPositions(command)
	if len(positions) == 0 {
		return command
	}

	out := command
	for j := len(positions) - 1; j >= 0; j-- {
		i := positions[j]
		before := out[:i]
		after := out[i:]
		trimmed := strings.TrimRight(before, " \t\r\n")
		if segmentHasStdioRedirect(trimmed) {
			continue
		}
		trailing := before[len(trimmed):]
		out = trimmed + backgroundJobStdioRedirect + trailing + after
	}
	return out
}

// PrepareShellCommandForExecute Combine the non-interactive wrapper and background IO redirection used for execute/exec.
// Must inject exec </dev/null first, then rewrite & the background segment, otherwise </dev/null inside the segment would make the stdin redirect be misjudged as already present.
func PrepareShellCommandForExecute(shellCommand string) string {
	return RedirectBackgroundJobStdio(PrepareNonInteractiveShellCommand(shellCommand))
}
