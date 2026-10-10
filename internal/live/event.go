package live

import (
	"context"
	"strings"
	"time"
)

// Gift is a normalized trusted Bilibili live gift event. It is emitted only
// from the authenticated Bilibili stream, never from a client API request.
type Gift struct {
	EventID  string `json:"event_id,omitempty"`
	Name     string `json:"name"`
	Count    int    `json:"count"`
	CoinType string `json:"coin_type,omitempty"`
	GiftID   int64  `json:"gift_id,omitempty"`
	GuardBuy bool   `json:"guard_buy,omitempty"`
}

type Event struct {
	Kind        string    `json:"kind,omitempty"`
	Gift        *Gift     `json:"gift,omitempty"`
	Platform    string    `json:"platform"`
	InstanceID  string    `json:"instance_id,omitempty"`
	UserID      string    `json:"user_id"`
	Username    string    `json:"username"`
	Content     string    `json:"content"`
	IsRoomAdmin bool      `json:"is_room_admin,omitempty"`
	IsAnchor    bool      `json:"is_anchor,omitempty"`
	GuardLevel  int       `json:"guard_level,omitempty"`
	Time        time.Time `json:"time"`
}

type Status struct {
	Platform  string    `json:"platform"`
	InstanceID string    `json:"instance_id,omitempty"`
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
