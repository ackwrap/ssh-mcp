package integration

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

const (
	maxCommandErrorRunes  = 2000
	maxCommandOutputBytes = 64 * 1024
)

var (
	ansiEscape       = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)
	privateKeyBlock  = regexp.MustCompile(`(?s)-----BEGIN [^-\r\n]*PRIVATE KEY-----.*?(?:-----END [^-\r\n]*PRIVATE KEY-----|\z)`)
	authCredential   = regexp.MustCompile(`(?i)\b(?:basic|bearer|digest|negotiate|aws4-hmac-sha256)\s+.*$`)
	namedCredential  = regexp.MustCompile(`(?i)["']?[A-Za-z0-9_-]*(?:api[_-]?key|private[_-]?key|access[_-]?token|refresh[_-]?token|token|secret|password|authorization)[A-Za-z0-9_-]*["']?\s*[:=]\s*`)
	credentialLikeID = regexp.MustCompile(`(?i)\b[A-Za-z0-9._-]*(?:api[_-]?key|secret|token|password)[A-Za-z0-9._-]*\b`)
)

type tailBuffer struct {
	mu        sync.Mutex
	data      []byte
	truncated bool
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	written := len(data)
	if len(data) >= maxCommandOutputBytes {
		discarded := len(data) > maxCommandOutputBytes || len(b.data) > 0
		b.data = append(b.data[:0], data[len(data)-maxCommandOutputBytes:]...)
		b.truncated = b.truncated || discarded
		return written, nil
	}
	overflow := len(b.data) + len(data) - maxCommandOutputBytes
	if overflow > 0 {
		copy(b.data, b.data[overflow:])
		b.data = b.data[:len(b.data)-overflow]
		b.truncated = true
	}
	b.data = append(b.data, data...)
	return written, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	prefix := ""
	if b.truncated {
		prefix = "[较早输出已截断]\n"
	}
	return prefix + string(b.data)
}

func combinedOutputLimited(process *exec.Cmd) (string, error) {
	var output tailBuffer
	process.Stdout = &output
	process.Stderr = &output
	err := process.Run()
	return output.String(), err
}

func safeCommandError(output string, err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "官方 CLI 执行超时或被取消"
	}
	detail := sanitizeCommandOutput(output)
	if detail == "" {
		detail = sanitizeCommandOutput(err.Error())
	}
	if detail == "" {
		return "官方 CLI 返回失败，但未提供错误详情"
	}
	return "官方 CLI 返回失败：" + detail
}

func sanitizeCommandOutput(output string) string {
	output = ansiEscape.ReplaceAllString(output, "")
	output = privateKeyBlock.ReplaceAllString(output, "[私钥已隐藏]")
	lines := strings.Split(output, "\n")
	redactNextValue := false
	for index, line := range lines {
		if redactNextValue && strings.TrimSpace(line) != "" {
			lines[index] = "******"
			redactNextValue = false
			continue
		}
		if match := namedCredential.FindStringIndex(line); match != nil {
			redactNextValue = strings.TrimSpace(line[match[1]:]) == ""
			line = line[:match[1]] + "******"
		}
		line = authCredential.ReplaceAllStringFunc(line, func(value string) string {
			if separator := strings.IndexAny(value, " \t"); separator >= 0 {
				return value[:separator] + " ******"
			}
			return "******"
		})
		lines[index] = credentialLikeID.ReplaceAllString(line, "[凭据已隐藏]")
	}
	output = strings.Join(lines, "\n")
	output = strings.Map(func(value rune) rune {
		if value == '\n' || value == '\t' || value >= ' ' {
			return value
		}
		return -1
	}, output)
	output = strings.TrimSpace(output)
	runes := []rune(output)
	if len(runes) > maxCommandErrorRunes {
		output = "…" + string(runes[len(runes)-maxCommandErrorRunes:])
	}
	return output
}
