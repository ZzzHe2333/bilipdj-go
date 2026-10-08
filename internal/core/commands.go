package core

import (
	"fmt"
	"github.com/ZzzHe2333/bilipdj-go/internal/live"
	"strconv"
	"strings"
	"time"
)

// Legacy queue operations are deliberately routed through one owner (App).
// Caller must hold a.mu. Identity is platform+user ID; legacy CSV rows fall
// back to the visible name to preserve cancel/edit behavior after migration.
func queueDisplayName(q QueueItem) string {
	name := q.Username
	for _, pre := range []string{"官|", "B|", "米|", "M|", "S|"} {
		name = strings.TrimPrefix(name, pre)
	}
	if strings.HasPrefix(name, "<") {
		if i := strings.Index(name, ">"); i > 1 {
			name = name[1:i]
		}
	}
	fields := strings.Fields(name)
	if len(fields) > 0 {
		return fields[0]
	}
	return ""
}
func (a *App) processDanmuCommandLocked(e live.Event) bool {
	input := strings.TrimSpace(e.Content)
	key := e.Platform + ":" + e.UserID
	if e.UserID == "" {
		key = e.Platform + ":" + e.Username
	}
	index := -1
	for i, item := range a.queue {
		if item.Key == key || (item.Platform == "legacy" && strings.EqualFold(queueDisplayName(item), e.Username)) {
			index = i
			break
		}
	}
	isAdmin := false
	for _, name := range a.config.Admins {
		if strings.EqualFold(strings.TrimSpace(name), e.Username) {
			isAdmin = true
			break
		}
	}
	// Named operators can perform the common legacy queue actions.
	if isAdmin {
		switch {
		case input == "完成":
			if len(a.queue) > 0 {
				a.queue = a.queue[1:]
				return true
			}
		case strings.HasPrefix(input, "删除 ") || strings.HasPrefix(input, "完成 ") || strings.HasPrefix(input, "del "):
			words := strings.Fields(input)
			if len(words) == 2 {
				if num, err := strconv.Atoi(words[1]); err == nil && num > 0 && num <= len(a.queue) {
					a.queue = append(a.queue[:num-1], a.queue[num:]...)
					return true
				}
			}
		case strings.HasPrefix(input, "新增 ") || strings.HasPrefix(input, "添加 ") || strings.HasPrefix(input, "add "):
			words := strings.SplitN(input, " ", 2)
			if len(words) == 2 && strings.TrimSpace(words[1]) != "" && (a.config.MaxQueue <= 0 || len(a.queue) < a.config.MaxQueue) {
				a.queue = append(a.queue, QueueItem{Key: fmt.Sprintf("admin:%d", time.Now().UnixNano()), Platform: "manual", Username: strings.TrimSpace(words[1]), At: e.Time})
				return true
			}
		}
	}
	if !a.config.AutoQueue || (a.config.Switches != nil && !a.config.Switches.Paidui) {
		return false
	}
	if index >= 0 {
		switch {
		case input == "取消排队" || input == "排队取消" || input == "我确认我取消排队":
			if a.config.Switches == nil || a.config.Switches.Cancel {
				a.queue = append(a.queue[:index], a.queue[index+1:]...)
				return true
			}
		case input == "替换" || input == "修改" || input == "内容洗白":
			if a.config.Switches == nil || a.config.Switches.Modify {
				a.queue[index].Note = ""
				return true
			}
		case strings.HasPrefix(input, "修改 ") || strings.HasPrefix(input, "替换 "):
			if a.config.Switches == nil || a.config.Switches.Modify {
				newNote := strings.TrimSpace(strings.SplitN(input, " ", 2)[1])
				if len([]rune(newNote)) > 200 {
					return false
				}
				a.queue[index].Note = newNote
				return true
			}
		}
		return false // no repeat entries
	}
	allowed := func(kind string) bool {
		if a.config.Switches == nil {
			return true
		}
		switch kind {
		case "官服":
			return a.config.Switches.Guanfu
		case "B服":
			return a.config.Switches.Bfu
		case "超级":
			return a.config.Switches.Chaoji
		case "米服":
			return a.config.Switches.Mifu
		}
		return true
	}
	prefix := ""
	mode := ""
	switch {
	case input == "官服排" || input == "排官服" || input == "官服排队" || input == "排队官服":
		mode = "官服"
	case input == "B服排" || input == "b服排" || input == "排b服" || input == "排B服" || input == "B服排队" || input == "排队B服" || input == "b服排队" || input == "排队b服":
		mode = "B服"
	case input == "超级排" || input == "超级排队":
		mode = "超级"
	case input == "小米排" || input == "排小米" || input == "排米服":
		mode = "米服"
	default:
		for _, pair := range []struct{ input, mode string }{{"官服排队 ", "官服"}, {"官服排 ", "官服"}, {"B服排 ", "B服"}, {"b服排 ", "B服"}, {"超级排队 ", "超级"}, {"超级排 ", "超级"}, {"米服排 ", "米服"}} {
			if strings.HasPrefix(input, pair.input) {
				prefix = pair.input
				mode = pair.mode
				break
			}
		}
	}
	if mode != "" && prefix == "" {
		prefix = input
	}
	if mode == "" {
		cmd := strings.TrimSpace(a.config.Command)
		if cmd == "" {
			cmd = "排队"
		}
		if input == "排队" || input == cmd {
			prefix = input
		} else if strings.HasPrefix(input, cmd+" ") || strings.HasPrefix(input, cmd+"：") || strings.HasPrefix(input, cmd+":") {
			prefix = cmd
		} else {
			return false
		}
	}
	if !allowed(mode) || (a.config.MaxQueue > 0 && len(a.queue) >= a.config.MaxQueue) {
		return false
	}
	note := strings.TrimLeft(strings.TrimSpace(strings.TrimPrefix(input, prefix)), " ：:")
	if len([]rune(note)) > 200 {
		return false
	}
	a.queue = append(a.queue, QueueItem{Key: key, Platform: e.Platform, UserID: e.UserID, Username: e.Username, Mode: mode, Note: note, At: e.Time})
	return true
}
