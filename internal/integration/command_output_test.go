package integration

import (
	"errors"
	"strings"
	"testing"
)

func TestSafeCommandErrorShowsUsefulDetailsAndRedactsCredentials(t *testing.T) {
	tests := []struct {
		name   string
		output string
		secret string
	}{
		{name: "API key", output: "API_KEY=TOP_SECRET", secret: "TOP_SECRET"},
		{name: "basic authorization", output: "Authorization: Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ==", secret: "QWxhZGRpbjpvcGVuIHNlc2FtZQ=="},
		{name: "single quoted authorization", output: `{'Authorization': 'Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ=='}`, secret: "QWxhZGRpbjpvcGVuIHNlc2FtZQ=="},
		{name: "multiline authorization", output: "Authorization:\nBasic QWxhZGRpbjpvcGVuIHNlc2FtZQ==", secret: "QWxhZGRpbjpvcGVuIHNlc2FtZQ=="},
		{name: "password with spaces", output: `password="correct horse battery staple"`, secret: "correct horse battery staple"},
		{name: "private key variable", output: "AWS_PRIVATE_KEY=private-value", secret: "private-value"},
		{name: "secret access key", output: "AWS_SECRET_ACCESS_KEY=aws-secret-value", secret: "aws-secret-value"},
		{name: "unterminated private key", output: "-----BEGIN PRIVATE KEY-----\nprivate-material", secret: "private-material"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			output := "\x1b[31mInvalid URL option\x1b[0m\n" + test.output
			message := safeCommandError(output, errors.New("exit status 1"))
			if !strings.Contains(message, "Invalid URL option") {
				t.Fatalf("safe error omitted useful detail: %q", message)
			}
			if strings.Contains(message, test.secret) || strings.Contains(message, "\x1b") {
				t.Fatalf("safe error leaked credential: %q", message)
			}
		})
	}
}

func TestTailBufferDoesNotMarkExactLimitAsTruncated(t *testing.T) {
	var output tailBuffer
	if _, err := output.Write([]byte(strings.Repeat("a", maxCommandOutputBytes))); err != nil {
		t.Fatal(err)
	}
	if result := output.String(); len(result) != maxCommandOutputBytes || strings.Contains(result, "较早输出已截断") {
		t.Fatalf("exact-limit output was marked truncated: length=%d", len(result))
	}
}

func TestTailBufferKeepsBoundedMostRecentOutput(t *testing.T) {
	var output tailBuffer
	prefix := strings.Repeat("a", maxCommandOutputBytes)
	suffix := "final-error"
	if _, err := output.Write([]byte(prefix)); err != nil {
		t.Fatal(err)
	}
	if _, err := output.Write([]byte(suffix)); err != nil {
		t.Fatal(err)
	}
	result := output.String()
	if len(output.data) != maxCommandOutputBytes || !strings.HasSuffix(result, suffix) || !strings.Contains(result, "较早输出已截断") {
		t.Fatalf("bounded output length=%d suffix=%v result-prefix=%q", len(output.data), strings.HasSuffix(result, suffix), result[:len("[较早输出已截断]")])
	}
}
