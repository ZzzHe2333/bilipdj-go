package core

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// LogEntry is a bounded, sanitized console log for the Windows-style log page.
// Login credentials and QR tickets are never part of the public log payload.
type LogEntry struct {
	ID       uint64    `json:"id"`
	Time     time.Time `json:"time"`
	Level    string    `json:"level"`
	Category string    `json:"category"`
	Message  string    `json:"message"`
}

const maxConsoleLogs = 500

var logSecrets = regexp.MustCompile(`(?i)(SESSDATA|bili_jct|DedeUserID(?:__ckMd5)?|cookie|authorization|auth_token|access_token|refresh_token|token|ticket|qrcode_key)\s*[:=]\s*([^\s;&]+)`)

func sanitizeConsoleLog(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " "))
	s = logSecrets.ReplaceAllString(s, "${1}=[已隐藏]")
	if utf8.RuneCountInString(s) > 500 {
		s = string([]rune(s)[:500]) + "…"
	}
	return s
}

// appendLogLocked must be called under App.mu; it shares the existing SSE hub.
func (a *App) appendLogLocked(level, category, message string) {
	message = sanitizeConsoleLog(message)
	if message == "" {
		return
	}
	a.logSequence++
	entry := LogEntry{ID: a.logSequence, Time: time.Now(), Level: level, Category: category, Message: message}
	a.logs = append(a.logs, entry)
	if len(a.logs) > maxConsoleLogs {
		a.logs = append([]LogEntry(nil), a.logs[len(a.logs)-maxConsoleLogs:]...)
	}
	a.publishLocked(Event{Type: "log", Data: entry})
}

func (a *App) logEvent(level, category, message string) {
	a.mu.Lock()
	a.appendLogLocked(level, category, message)
	a.mu.Unlock()
}
