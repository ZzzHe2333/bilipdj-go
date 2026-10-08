package live

import (
	"context"
	"strings"
	"time"
)

type Event struct {
	Platform string    `json:"platform"`
	UserID   string    `json:"user_id"`
	Username string    `json:"username"`
	Content  string    `json:"content"`
	Time     time.Time `json:"time"`
}

type Status struct {
	Platform  string    `json:"platform"`
	Connected bool      `json:"connected"`
	Message   string    `json:"message"`
	Since     time.Time `json:"since"`
}

type Emit func(Event)
type Report func(Status)

// Source runs until its context is canceled. It reconnects internally.
type Source interface {
	Run(ctx context.Context, room, cookie string, emit Emit, report Report)
}

func CleanID(s string) string { return strings.TrimSpace(s) }
