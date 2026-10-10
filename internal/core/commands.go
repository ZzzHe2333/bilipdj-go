package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ZzzHe2333/bilipdj-go/internal/live"
)

// The queue and the role/config updates share the App lock and save path.
func queueDisplayName(q QueueItem) string {
	name := q.Username
	for _, p := range []string{"官|", "B|", "米|", "M|", "S|"} {
		name = strings.TrimPrefix(name, p)
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
func named(names []string, name string) bool {
	for _, s := range names {
		if strings.EqualFold(strings.TrimSpace(s), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}
func removeName(names []string, who string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		if !strings.EqualFold(strings.TrimSpace(name), who) {
			out = append(out, name)
		}
	}
	return out
}
func (a *App) stripBlacklistedRolesLocked() {
	for _, name := range a.config.Blacklist {
		a.config.Admins = removeName(a.config.Admins, name)
		a.config.SuperAdmins = removeName(a.config.SuperAdmins, name)
		a.config.Guards = removeName(a.config.Guards, name)
	}
}
func (a *App) isSuperOperator(e live.Event) bool {
 if a.isBlacklisted(e){return false}
 if e.Platform=="bilibili"&&e.IsAnchor{return true}
 p,ok:=a.config.effectiveEntry(e)
 if ok{return p.Role=="super_admin"}
 if a.config.Permissions!=nil{return false}
 return named(a.config.SuperAdmins,e.Username)
}
func (a *App) isOperator(e live.Event) bool {
 if a.isBlacklisted(e){return false}
 if a.isSuperOperator(e){return true}
 p,ok:=a.config.effectiveEntry(e)
 if ok{return (p.Role=="admin"||p.Role=="part_time")&&len(p.Capabilities)>0}
 if a.config.Permissions!=nil{return e.Platform=="bilibili"&&e.IsRoomAdmin&&a.config.Switches!=nil&&a.config.Switches.RoomAdminOperator}
 return named(a.config.Admins,e.Username)||(e.Platform=="bilibili"&&e.IsRoomAdmin&&a.config.Switches!=nil&&a.config.Switches.RoomAdminOperator)
}
func (a *App) isGuard(e live.Event) bool {
 if e.Platform=="bilibili"&&e.GuardLevel>0{return true}
 if a.config.Permissions!=nil{return false}
 return named(a.config.Guards,e.Username)
}
func (a *App) dailyPeriodAt(now time.Time) string {
	local := now.In(time.Local)
	reset := a.config.DailyQueueResetTime
	if len(reset) != 5 {
		reset = "04:00"
	}
	hh, _ := strconv.Atoi(reset[:2])
	mm, _ := strconv.Atoi(reset[3:])
	resetToday := time.Date(local.Year(), local.Month(), local.Day(), hh, mm, 0, 0, local.Location())
	if local.Before(resetToday) {
		local = local.AddDate(0, 0, -1)
	}
	return local.Format("2006-01-02") + "@" + reset
}
func dailyIdentity(e live.Event) string {
	if id := strings.TrimSpace(e.UserID); id != "" {
		return e.Platform + ":" + id
	}
	hash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(e.Username))))
	return e.Platform + ":name-" + hex.EncodeToString(hash[:12])
}
func (a *App) dailyCanJoin(e live.Event) bool {
	if a.config.DailyQueueLimit == 0 {
		return true
	}
	period := a.dailyPeriodAt(e.Time)
	if period != a.dailyPeriod {
		return true
	}
	return a.dailyCounts[dailyIdentity(e)] < a.config.DailyQueueLimit
}
func (a *App) dailyMarkJoin(e live.Event) {
	if a.config.DailyQueueLimit == 0 {
		return
	}
	period := a.dailyPeriodAt(e.Time)
	if period != a.dailyPeriod {
		a.dailyPeriod = period
		a.dailyCounts = map[string]int{}
	}
	a.dailyCounts[dailyIdentity(e)]++
}
func (a *App) appendManualLocked(name string, at time.Time) bool {
	if name == "" || len([]rune(name)) > 200 || len(a.queue) >= 10000 {
		return false
	}
	a.queue = append(a.queue, QueueItem{Key: fmt.Sprintf("admin:%d", time.Now().UnixNano()), Platform: "manual", Username: name, At: at})
	return true
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
	operator := a.isOperator(e)
	super := a.isSuperOperator(e)
	if operator {
		if super {
			switch {
			case strings.HasPrefix(input, "添加管理员 "):
				target := strings.TrimSpace(strings.TrimPrefix(input, "添加管理员 "))
				if target!=""&&len([]rune(target))<=60{return a.upsertRoleLocked(e.Platform,target,"admin")}
				return false
			case strings.HasPrefix(input, "取消管理员 "):
				target := strings.TrimSpace(strings.TrimPrefix(input, "取消管理员 "))
				if a.removeRoleLocked(e.Platform,target,"admin"){return true}
				return false
			}
		}
		switch {
		case strings.HasPrefix(input, "拉黑 "):
			target := strings.TrimSpace(strings.TrimPrefix(input, "拉黑 "))
			if a.commandAllowed(e,"moderate")&&a.upsertRoleLocked(e.Platform,target,"blacklist"){return true}
			return false
		case strings.HasPrefix(input, "取消拉黑 "):
			target := strings.TrimSpace(strings.TrimPrefix(input, "取消拉黑 "))
			if a.commandAllowed(e,"moderate")&&a.removeRoleLocked(e.Platform,target,"blacklist"){return true}
			return false
		case input == "暂停排队功能" || input == "关闭自助排队":
            if !a.commandAllowed(e,"switch"){return false}
			a.config.Switches.Paidui = false
			return true
		case input == "恢复排队功能" || input == "恢复自助排队":
            if !a.commandAllowed(e,"switch"){return false}
			a.config.Switches.Paidui = true
			return true
		case input == "开启舰长插队":
            if !a.commandAllowed(e,"switch"){return false}
			a.config.Switches.GuardInsert = true
			return true
		case input == "关闭舰长插队":
            if !a.commandAllowed(e,"switch"){return false}
			a.config.Switches.GuardInsert = false
			return true
		case input == "允许房管成为插件管理员":
            if !a.commandAllowed(e,"switch"){return false}
			a.config.Switches.RoomAdminOperator = true
			return true
		case input == "停止房管成为插件管理员":
            if !a.commandAllowed(e,"switch"){return false}
			a.config.Switches.RoomAdminOperator = false
			return true
		case strings.HasPrefix(input, "设置排队人数") || strings.HasPrefix(input, "设置排队上限"):
            if !a.commandAllowed(e,"limits"){return false}
			for _, prefix := range []string{"设置排队人数上限", "设置排队人数", "设置排队上限"} {
				if strings.HasPrefix(input, prefix) {
					tail := strings.TrimSpace(strings.TrimPrefix(input, prefix))
					if n, err := strconv.Atoi(tail); err == nil && n >= 1 && n <= 10000 {
						a.config.MaxQueue = n
						return true
					}
					return false
				}
			}
		case input == "完成":
            if !a.commandAllowed(e,"queue"){return false}
			if len(a.queue) > 0 {
				a.queue = a.queue[1:]
				return true
			}
		case strings.HasPrefix(input, "删除 ") || strings.HasPrefix(input, "完成 ") || strings.HasPrefix(input, "del "):
            if !a.commandAllowed(e,"queue"){return false}
			words := strings.Fields(input)
			if len(words) == 2 {
				if n, err := strconv.Atoi(words[1]); err == nil && n > 0 && n <= len(a.queue) {
					a.queue = append(a.queue[:n-1], a.queue[n:]...)
					return true
				}
			}
		case strings.HasPrefix(input, "新增 ") || strings.HasPrefix(input, "添加 ") || strings.HasPrefix(input, "add "):
            if !a.commandAllowed(e,"queue"){return false}
			words := strings.SplitN(input, " ", 2)
			if len(words) == 2 {
				return a.appendManualLocked(strings.TrimSpace(words[1]), e.Time)
			}
		case strings.HasPrefix(input, "无影插 ") || strings.HasPrefix(input, "插队 "):
            if !a.commandAllowed(e,"insert"){return false}
			words := strings.SplitN(input, " ", 3)
			if len(words) == 3 {
				n, err := strconv.Atoi(words[1])
				if err == nil && n > 0 && n <= 30 && (words[0] != "无影插" || n <= 20) && n <= len(a.queue)+1 {
					name := strings.TrimSpace(words[2])
					if name != "" && len([]rune(name)) <= 200 && len(a.queue) < 10000 {
						if words[0] == "插队" {
							name = "@" + name
						}
						item := QueueItem{Key: fmt.Sprintf("admin:%d", time.Now().UnixNano()), Platform: "manual", Username: name, At: e.Time}
						a.queue = append(a.queue, QueueItem{})
						copy(a.queue[n:], a.queue[n-1:len(a.queue)-1])
						a.queue[n-1] = item
						return true
					}
				}
			}
		}
	}
	if input == "插队" && a.config.AutoQueue && (a.config.Switches == nil || a.config.Switches.Paidui) && a.useGiftCreditLocked(e, index) {
		return true
	}
	if a.config.GiftQueue != nil && a.config.GiftQueue.GiftOnly && !operator && index < 0 {
		return false
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
				note := strings.TrimSpace(strings.SplitN(input, " ", 2)[1])
				if len([]rune(note)) <= 200 {
					a.queue[index].Note = note
					return true
				}
			}
		}
		return false
	}
	if input == "插队" && a.config.Switches != nil && a.config.Switches.GuardInsert && a.isGuard(e) {
		if len(a.queue) >= 10000 {
			return false
		}
		idx := len(a.queue)
		for idx > 0 && (a.queue[idx-1].Guard || named(a.config.Guards, queueDisplayName(a.queue[idx-1]))) {
			idx--
		}
		item := QueueItem{Key: key, Platform: e.Platform, UserID: e.UserID, Username: e.Username, At: e.Time, Guard: true}
		a.queue = append(a.queue, QueueItem{})
		copy(a.queue[idx+1:], a.queue[idx:len(a.queue)-1])
		a.queue[idx] = item
		return true
	}
	allowed := func(mode string) bool {
		if a.config.Switches == nil {
			return true
		}
		switch mode {
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
	switch input {
	case "官服排", "排官服", "官服排队", "排队官服":
		mode = "官服"
	case "B服排", "b服排", "排b服", "排B服", "B服排队", "排队B服", "b服排队", "排队b服":
		mode = "B服"
	case "超级排", "超级排队":
		mode = "超级"
	case "小米排", "排小米", "排米服":
		mode = "米服"
	default:
		for _, p := range []struct{ input, mode string }{{"官服排队 ", "官服"}, {"官服排 ", "官服"}, {"B服排 ", "B服"}, {"b服排 ", "B服"}, {"超级排队 ", "超级"}, {"超级排 ", "超级"}, {"米服排 ", "米服"}} {
			if strings.HasPrefix(input, p.input) {
				prefix = p.input
				mode = p.mode
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
	if !allowed(mode) || len(a.queue) >= 10000 || (a.config.MaxQueue > 0 && len(a.queue) >= a.config.MaxQueue && !operator) || (!operator && !a.dailyCanJoin(e)) {
		return false
	}
	note := strings.TrimLeft(strings.TrimSpace(strings.TrimPrefix(input, prefix)), " ：:")
	if len([]rune(note)) > 200 {
		return false
	}
	a.queue = append(a.queue, QueueItem{Key: key, Platform: e.Platform, UserID: e.UserID, Username: e.Username, Mode: mode, Note: note, At: e.Time})
	if !operator {
		a.dailyMarkJoin(e)
	}
	return true
}
