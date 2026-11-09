package mihomo

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/nelvko/lazyclash/internal/diagnostic"
)

const MaxLogMessage = 4096
const maxLogFrame = 64 << 10

// LogEntry.Time is the local observation time; Mihomo's default /logs format
// contains type and payload only. Entries are live observations, not history.
type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

var logURL = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+.-]*://[^\s<>"']+`)
var logCredential = regexp.MustCompile(`(?i)\b(?:authorization|proxy-authorization|password|passwd|token|secret|api[-_]?key|access[-_]?token|client[-_]?secret)["']?\s*(?::|=|\s)\s*.+`)
var logAuthorization = regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[a-z0-9._~+/=-]+`)
var logUserinfo = regexp.MustCompile(`[^\s:]+:[^\s@]+@[^\s]+`)

// SafeLogMessage conservatively hides complete URLs and credential-bearing
// fields, including unknown token query names and private URL path segments.
// Logs are unstructured; this is intentionally stricter than URL previews.
func SafeLogMessage(message, controllerSecret string) string {
	if controllerSecret != "" {
		message = strings.ReplaceAll(message, controllerSecret, "[redacted]")
	}
	message = ansi.Strip(strings.ToValidUTF8(message, "�"))
	// Logs may themselves contain a JSON-escaped object or URL. Normalize the
	// quoting/slash escapes before applying the credential rules to that text.
	message = strings.NewReplacer(`\"`, `"`, `\'`, `'`, `\/`, `/`).Replace(message)
	message = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return ' '
		}
		return r
	}, message)
	if controllerSecret != "" {
		message = strings.ReplaceAll(message, controllerSecret, "[redacted]")
	}
	message = logURL.ReplaceAllString(message, "[URL redacted]")
	message = logCredential.ReplaceAllString(message, "[credentials redacted]")
	message = logAuthorization.ReplaceAllString(message, "[authorization redacted]")
	message = logUserinfo.ReplaceAllString(message, "[userinfo redacted]")
	if len(message) > MaxLogMessage {
		message = message[:MaxLogMessage]
		for !utf8.ValidString(message) {
			message = message[:len(message)-1]
		}
		message += "…"
	}
	return message
}

// StreamLogs owns one authenticated, cancellable, live WebSocket connection. It
// never writes raw messages to disk. emit runs serially and must return promptly.
func (c Client) StreamLogs(ctx context.Context, emit func(LogEntry)) error {
	if emit == nil {
		return diagnostic.New("diagnostic.logs.consumer", "a log consumer is required")
	}
	return c.streamJSON(ctx, "/logs?level=info", maxLogFrame, 0, func(raw []byte) error {
		var event struct {
			Type    *string `json:"type"`
			Payload *string `json:"payload"`
		}
		if json.Unmarshal(raw, &event) != nil || event.Type == nil || event.Payload == nil {
			return diagnostic.New("diagnostic.logs.event", "invalid Mihomo log event")
		}
		switch *event.Type {
		case "debug", "info", "warning", "error", "fatal":
		default:
			return diagnostic.New("diagnostic.logs.level", "invalid Mihomo log level")
		}
		emit(LogEntry{Time: time.Now(), Level: *event.Type, Message: SafeLogMessage(*event.Payload, c.Secret)})
		return nil
	})
}
