package core

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The legacy format is intentionally parsed without executing code or loading external resources.
// Only explicitly recognized settings are mapped to active Go behavior; original input is archived unchanged.
const maxLegacyBytes = 12 << 20

type LegacyPreview struct {
	Files          []string       `json:"files"`
	Mapped         []string       `json:"mapped"`
	Warnings       []string       `json:"warnings"`
	QueueCount     int            `json:"queue_count"`
	SlotCounts     map[string]int `json:"slot_counts,omitempty"`
	BlacklistCount int            `json:"blacklist_count"`
}
type legacyMigration struct {
	preview     LegacyPreview
	cfg         Config
	style       map[string]any
	appearance  map[string]any
	queue       []QueueItem
	slots       map[string][]QueueItem
	dailyPeriod string
	dailyCounts map[string]int
}

func defaultConfig() Config {
	return Config{Bilibili: PlatformConfig{Room: "3049445"}, AutoQueue: true, Command: "排队", MaxQueue: 100, DailyQueueResetTime: "04:00", Language: "中文", ArchiveSlot: 1, GiftQueue: defaultGiftSettings(), Switches: defaultSwitches(), Admins: []string{}, Blacklist: []string{}}
}
func defaultStyle() map[string]any {
	return map[string]any{"bg1": "#0e2036", "bg2": "#060b14", "bg3": "#020409", "text_color": "#eaf6ff", "queue_font_size": 50, "queue_font_weight": "700", "queue_font_style": "normal", "queue_font_family": "Microsoft YaHei, Noto Sans SC, PingFang SC, sans-serif", "queue_letter_spacing": 0, "queue_word_spacing": 0, "queue_line_height": "1.20", "queue_item_gap": 10, "queue_text_align": "left", "queue_text_opacity": 100, "queue_item_padding_x": 14, "queue_item_padding_y": 8, "text_grad_start": "#f7f7f7", "text_grad_end": "rgba(255,255,255,0.6)", "text_stroke_color": "#000000", "text_stroke_enabled": true, "auto_scroll": false, "show_sequence": false}
}
func defaultAppearance() map[string]any {
	return map[string]any{"schema": 1, "design": "aurora", "mode": "dark", "font_family": "Microsoft YaHei UI", "font_size": 10, "radius": 10,
		"dark":  map[string]any{"background": "#090E1A", "sidebar": "#0D1424", "surface": "#111A2C", "surface_alt": "#18233A", "input": "#0D1424", "border": "#26334D", "text": "#E6EDF7", "muted": "#8A9AB3", "accent": "#7C6CF2", "accent_hover": "#9184FF", "selection": "#7C6CF2", "success": "#32D583", "warning": "#F5B942", "danger": "#F97066"},
		"light": map[string]any{"background": "#F4F6FB", "sidebar": "#EAEDF5", "surface": "#FFFFFF", "surface_alt": "#F0EFFF", "input": "#FBFBFE", "border": "#D5D9E7", "text": "#20263A", "muted": "#687089", "accent": "#6757D9", "accent_hover": "#5142BC", "selection": "#6757D9", "success": "#007A40", "warning": "#B57600", "danger": "#D92D20"}}
}

type yamlLine struct {
	indent  int
	content string
}

func unquotedComment(raw string) string {
	sq, dq, esc := false, false, false
	for i, r := range raw {
		if esc {
			esc = false
			continue
		}
		if r == '\\' && dq {
			esc = true
			continue
		}
		if r == '\'' && !dq {
			sq = !sq
			continue
		}
		if r == '"' && !sq {
			dq = !dq
			continue
		}
		if r == '#' && !sq && !dq && (i == 0 || raw[i-1] == ' ' || raw[i-1] == '\t') {
			return strings.TrimSpace(raw[:i])
		}
	}
	return strings.TrimSpace(raw)
}
func scalarValue(s string) any {
	s = strings.TrimSpace(s)
	if s == "[]" {
		return []any{}
	}
	if s == "{}" {
		return map[string]any{}
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		if v, e := strconv.Unquote(s); e == nil {
			return v
		}
		return s[1 : len(s)-1]
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return strings.ReplaceAll(s[1:len(s)-1], "''", "'")
	}
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "null", "none", "~":
		return nil
	}
	if n, e := strconv.ParseInt(s, 10, 64); e == nil {
		return n
	}
	if f, e := strconv.ParseFloat(s, 64); e == nil {
		return f
	}
	return s
}
func parseYAML(data []byte) (map[string]any, error) {
	if len(data) > maxLegacyBytes || !utf8.Valid(data) {
		return nil, errors.New("invalid YAML input")
	}
	lines := []yamlLine{}
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.Contains(raw, "\t") && strings.TrimLeft(raw, " \t") != raw {
			return nil, errors.New("YAML tabs are unsupported")
		}
		content := unquotedComment(strings.TrimSpace(raw))
		if content == "" || content == "---" || content == "..." {
			continue
		}
		lines = append(lines, yamlLine{indent: len(raw) - len(strings.TrimLeft(raw, " ")), content: content})
	}
	if len(lines) == 0 {
		return nil, errors.New("empty YAML")
	}
	pos := 0
	var parse func(indent, depth int) (any, error)
	parse = func(indent, depth int) (any, error) {
		if depth > 24 {
			return nil, errors.New("YAML nesting too deep")
		}
		if pos >= len(lines) {
			return map[string]any{}, nil
		}
		isList := strings.HasPrefix(lines[pos].content, "- ")
		if isList {
			vals := []any{}
			for pos < len(lines) && lines[pos].indent == indent {
				s := lines[pos].content
				if !strings.HasPrefix(s, "- ") {
					return nil, errors.New("mixed list/map YAML block")
				}
				item := strings.TrimSpace(strings.TrimPrefix(s, "- "))
				pos++
				if item == "" && pos < len(lines) && lines[pos].indent > indent {
					v, e := parse(lines[pos].indent, depth+1)
					if e != nil {
						return nil, e
					}
					vals = append(vals, v)
				} else {
					vals = append(vals, scalarValue(item))
				}
			}
			return vals, nil
		}
		obj := map[string]any{}
		for pos < len(lines) && lines[pos].indent == indent {
			s := lines[pos].content
			idx := strings.IndexByte(s, ':')
			if idx <= 0 {
				return nil, fmt.Errorf("invalid YAML entry: %s", s)
			}
			key := strings.TrimSpace(s[:idx])
			if strings.HasPrefix(key, "- ") {
				return nil, errors.New("mixed map/list YAML block")
			}
			if key == "" {
				return nil, errors.New("empty YAML key")
			}
			rest := strings.TrimSpace(s[idx+1:])
			pos++
			if rest == "" && pos < len(lines) && lines[pos].indent > indent {
				v, e := parse(lines[pos].indent, depth+1)
				if e != nil {
					return nil, e
				}
				obj[key] = v
			} else if rest == "" {
				obj[key] = map[string]any{}
			} else {
				obj[key] = scalarValue(rest)
			}
		}
		return obj, nil
	}
	v, e := parse(lines[0].indent, 0)
	if e != nil {
		return nil, e
	}
	if pos != len(lines) {
		return nil, errors.New("unsupported YAML indentation")
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("expected YAML mapping")
	}
	return obj, nil
}
func mget(m map[string]any, k string) map[string]any {
	if v, ok := m[k].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
func truth(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(x, "true") || x == "1"
	case int64:
		return x != 0
	case float64:
		return x != 0
	}
	return false
}
func number(v any, def int) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int64:
		return int(x)
	case int:
		return x
	case string:
		if n, e := strconv.Atoi(x); e == nil {
			return n
		}
	}
	return def
}
func stringList(v any) []string {
	list := []string{}
	seen := map[string]bool{}
	switch vv := v.(type) {
	case []any:
		for _, x := range vv {
			n := str(x)
			if n != "" && !seen[n] {
				list = append(list, n)
				seen[n] = true
			}
		}
	case []string:
		for _, x := range vv {
			n := str(x)
			if n != "" && !seen[n] {
				list = append(list, n)
				seen[n] = true
			}
		}
	}
	return list
}
func queueCSV(data []byte) ([]QueueItem, error) {
	reader := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	reader.FieldsPerRecord = -1
	rows, e := reader.ReadAll()
	if e != nil {
		return nil, e
	}
	items := []QueueItem{}
	ts := time.Now()
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		idx, e := strconv.Atoi(strings.TrimSpace(row[0]))
		if e != nil || idx < 1 {
			continue
		}
		id := strings.TrimSpace(row[1])
		note := ""
		if len(row) >= 3 {
			note = strings.TrimSpace(row[2])
		}
		if id == "" {
			continue
		}
		items = append(items, QueueItem{Key: fmt.Sprintf("legacy:%d:%d", idx, len(items)), Platform: "legacy", Username: id, Note: note, At: ts})
		if len(items) > 10000 {
			return nil, errors.New("legacy queue too large")
		}
	}
	return items, nil
}
func parseLegacy(name string, body []byte, current Config) (legacyMigration, error) {
	m := legacyMigration{cfg: current, slots: map[string][]QueueItem{}, preview: LegacyPreview{Files: []string{}, Mapped: []string{}, Warnings: []string{}, SlotCounts: map[string]int{}}}
	// Preview must never change the live configuration through shared pointers.
	if current.GiftQueue != nil {
		gs := *current.GiftQueue
		gs.Names = append([]string{}, current.GiftQueue.Names...)
		m.cfg.GiftQueue = &gs
	} else {
		m.cfg.GiftQueue = defaultGiftSettings()
	}
	if current.Switches != nil {
		v := *current.Switches
		m.cfg.Switches = &v
	} else {
		m.cfg.Switches = defaultSwitches()
	}
	m.cfg.Admins = append([]string{}, current.Admins...)
	m.cfg.SuperAdmins = append([]string{}, current.SuperAdmins...)
	m.cfg.Guards = append([]string{}, current.Guards...)
	m.cfg.Blacklist = append([]string{}, current.Blacklist...)
	if len(body) > maxLegacyBytes {
		return m, errors.New("legacy import exceeds 12 MiB")
	}
	docs := map[string][]byte{}
	if strings.HasSuffix(strings.ToLower(name), ".zip") {
		z, e := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if e != nil {
			return m, e
		}
		total := uint64(0)
		if len(z.File) > 300 {
			return m, errors.New("legacy ZIP too many files")
		}
		for _, f := range z.File {
			if f.FileInfo().IsDir() {
				continue
			}
			if f.UncompressedSize64 > maxLegacyBytes {
				return m, errors.New("legacy ZIP oversized entry")
			}
			total += f.UncompressedSize64
			if total > maxLegacyBytes {
				return m, errors.New("legacy ZIP exceeds 12 MiB uncompressed")
			}
			clean := path.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
			if strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, "/../") || strings.Contains(clean, ":") {
				return m, errors.New("unsafe legacy ZIP path")
			}
			if _, ok := docs[clean]; ok {
				return m, errors.New("duplicate legacy file name")
			}
			rc, e := f.Open()
			if e != nil {
				return m, e
			}
			b, e := io.ReadAll(io.LimitReader(rc, maxLegacyBytes+1))
			rc.Close()
			if e != nil {
				return m, e
			}
			docs[clean] = b
		}
	} else {
		docs[path.Base(name)] = body
	}
	if len(docs) == 0 {
		return m, errors.New("empty legacy archive")
	}
	ordered := make([]string, 0, len(docs))
	for n := range docs {
		ordered = append(ordered, n)
	}
	// Process settings before queue, with priority for a matching active slot.
	for _, base := range []string{"config.yaml", "quanxian.yaml", "kaiguan.yaml", "style.json", "appearance.json", "blacklist.csv", "queue_archive_state.json"} {
		for _, n := range ordered {
			if strings.EqualFold(path.Base(n), base) {
				m.preview.Files = append(m.preview.Files, n)
				b := docs[n]
				switch base {
				case "config.yaml":
					obj, e := parseYAML(b)
					if e != nil {
						return m, fmt.Errorf("config.yaml: %w", e)
					}
					bl := mget(obj, "bilibili")
					dy := mget(obj, "douyin")
					mj := mget(obj, "myjs")
					ui := mget(obj, "ui")
					pa := mget(obj, "platforms")
					if v := bl["roomid"]; v != nil {
						m.cfg.Bilibili.Room = str(v)
					}
					if v := bl["cookie"]; v != nil {
						m.cfg.Bilibili.Cookie = str(v)
					}
					if v := dy["live_id"]; v != nil {
						m.cfg.Douyin.Room = str(v)
					}
					if v := dy["cookie"]; v != nil {
						m.cfg.Douyin.Cookie = str(v)
					}
					if v, ok := dy["enabled"]; ok {
						m.cfg.Douyin.Enabled = truth(v)
					}
					p := str(obj["platform"])
					if p == "bilibili" {
						m.cfg.Bilibili.Enabled = true
					}
					if p == "douyin" {
						m.cfg.Douyin.Enabled = true
					}
					// Multi-platform active list may appear in newer backup layouts.
					for _, s := range stringList(pa["active"]) {
						if s == "bilibili" {
							m.cfg.Bilibili.Enabled = true
						}
						if s == "douyin" {
							m.cfg.Douyin.Enabled = true
						}
					}
					if v, ok := mj["paidui_list_length_max"]; ok {
						m.cfg.MaxQueue = number(v, 100)
					}
					if v, ok := mj["all_suoyourenbukepaidui"]; ok {
						m.cfg.AutoQueue = !truth(v)
					}
					if v, ok := mj["admins"]; ok {
						m.cfg.Admins = stringList(v)
					}
					if v, ok := mj["jianzhang"]; ok {
						m.cfg.Guards = stringList(v)
					}
					if v, ok := mj["gift_queue_enabled"]; ok {
						m.cfg.GiftQueue.Enabled = truth(v)
					}
					if v, ok := mj["gift_queue_names"]; ok {
						m.cfg.GiftQueue.Names = stringList(v)
					}
					if v, ok := mj["gift_queue_min_batteries"]; ok {
						m.cfg.GiftQueue.MinBatteries = number(v, 0)
					}
					if v, ok := mj["gift_queue_allow_multiple"]; ok {
						m.cfg.GiftQueue.AllowMultiple = truth(v)
					}
					if v, ok := mj["gift_queue_slots_per_gift"]; ok {
						m.cfg.GiftQueue.SlotsPerGift = number(v, 1)
					}
					if v, ok := mj["gift_queue_insert_rank"]; ok {
						m.cfg.GiftQueue.InsertRank = number(v, 1)
					}
					if v, ok := mj["gift_queue_only"]; ok {
						m.cfg.GiftQueue.GiftOnly = truth(v)
					}
					if v, ok := mj["daily_queue_limit"]; ok {
						m.cfg.DailyQueueLimit = number(v, 0)
					}
					if v, ok := mj["daily_queue_reset_time"]; ok {
						m.cfg.DailyQueueResetTime = str(v)
					}
					if v, ok := mj["daily_queue_period"]; ok {
						m.dailyPeriod = str(v)
					}
					if v, ok := mj["daily_queue_counts"]; ok {
						m.dailyCounts = parseLegacyDailyCounts(v)
					}
					if v, ok := ui["language"]; ok {
						m.cfg.Language = str(v)
					}
					if v, ok := mget(obj, "queue_archive")["active_slot"]; ok {
						m.cfg.ArchiveSlot = number(v, 1)
					}
					// Older configuration schema stores permissions and switches under config.yaml.
					if v := mget(obj, "quanxian")["admin"]; v != nil && len(m.cfg.Admins) == 0 {
						m.cfg.Admins = stringList(v)
					}
					if switches := mget(obj, "kaiguan"); len(switches) > 0 {
						applyLegacySwitches(&m.cfg, switches)
					}
					if x := mget(obj, "quanxian"); len(x) > 0 {
						if v, ok := x["super_admin"]; ok {
							m.cfg.SuperAdmins = stringList(v)
						}
						if v, ok := x["jianzhang"]; ok {
							m.cfg.Guards = stringList(v)
						}
						if admins := stringList(x["admin"]); len(admins) > 0 {
							m.cfg.Admins = admins
						}
						if blacklist := stringList(x["blacklist"]); len(blacklist) > 0 {
							m.cfg.Blacklist = blacklist
						}
					}
					m.preview.Mapped = append(m.preview.Mapped, "平台直播间/Cookie/启用状态", "队列上限、每日排队次数/重置时间/计数", "管理员/超管/舰长身份与黑名单", "语言与存档槽位", "已支持的功能开关", "礼物资格设置（不自动导入旧版已使用/已消费记录）")
					m.preview.Warnings = append(m.preview.Warnings, "旧版礼物已使用 UID / 待消费积分不自动转移（避免跨版本错误重复授权）；回调和第三方平台保留原始备份；旧版每日用户计数的纯数字 UID 暂映射到 B站 UID")
				case "quanxian.yaml":
					obj, e := parseYAML(b)
					if e != nil {
						return m, fmt.Errorf("quanxian.yaml: %w", e)
					}
					if _, ok := obj["admin"]; ok {
						m.cfg.Admins = stringList(obj["admin"])
					}
					if _, ok := obj["admins"]; ok {
						m.cfg.Admins = stringList(obj["admins"])
					}
					if _, ok := obj["guanliyuan"]; ok {
						m.cfg.Admins = stringList(obj["guanliyuan"])
					}
					if v, ok := obj["super_admin"]; ok {
						m.cfg.SuperAdmins = stringList(v)
					}
					if v, ok := obj["jianzhang"]; ok {
						m.cfg.Guards = stringList(v)
					}
					m.preview.Mapped = append(m.preview.Mapped, "权限文件（管理员/最高管理员/舰长）")
					if blacklist := stringList(obj["blacklist"]); len(blacklist) > 0 {
						m.cfg.Blacklist = blacklist
					}
				case "kaiguan.yaml":
					obj, e := parseYAML(b)
					if e != nil {
						return m, fmt.Errorf("kaiguan.yaml: %w", e)
					}
					applyLegacySwitches(&m.cfg, obj)
					m.preview.Mapped = append(m.preview.Mapped, "功能开关：总排队/官服/B服/超级/米服/取消/修改")
					m.preview.Mapped = append(m.preview.Mapped, "舰长插队/房管操作开关")
				case "style.json", "appearance.json":
					obj := map[string]any{}
					if e := json.Unmarshal(b, &obj); e != nil {
						return m, fmt.Errorf("%s: %w", base, e)
					}
					if base == "style.json" {
						m.style = obj
					} else {
						m.appearance = obj
					}
					m.preview.Mapped = append(m.preview.Mapped, base+" 外观参数")
				case "blacklist.csv":
					entries, e := queueCSV(b)
					if e != nil {
						return m, e
					}
					m.cfg.Blacklist = []string{}
					for _, q := range entries {
						m.cfg.Blacklist = append(m.cfg.Blacklist, q.Username)
					}
					m.preview.Mapped = append(m.preview.Mapped, "黑名单")
				case "queue_archive_state.json":
					m.preview.Mapped = append(m.preview.Mapped, "排队存档状态（槽位由配置决定）")
				}
			}
		}
	}
	// Recognize all ten original queue_archive_slot_N.csv snapshots.
	for _, n := range ordered {
		base := strings.ToLower(path.Base(n))
		if !strings.HasSuffix(base, ".csv") || base == "blacklist.csv" {
			continue
		}
		num := 0
		for _, format := range []string{"queue_archive_slot_%d.csv", "cdang_%d.csv", "%d.csv"} {
			for i := 1; i <= 10; i++ {
				if base == fmt.Sprintf(format, i) {
					num = i
					break
				}
			}
			if num > 0 {
				break
			}
		}
		if num == 0 {
			continue
		}
		entries, err := queueCSV(docs[n])
		if err != nil {
			return m, fmt.Errorf("%s: %w", n, err)
		}
		m.slots[slotKey(num)] = entries
		m.preview.Files = append(m.preview.Files, n)
		m.preview.SlotCounts[slotKey(num)] = len(entries)
		if num == m.cfg.ArchiveSlot {
			m.queue = entries
		}
	}
	if len(m.slots) > 0 {
		m.preview.Mapped = append(m.preview.Mapped, "10槽位排队 CSV 存档")
	}
	if len(m.preview.Files) == 0 {
		return m, errors.New("ZIP 没有可识别的旧版配置文件")
	}
	cfg, e := cleanConfig(m.cfg)
	if e != nil {
		return m, fmt.Errorf("旧配置参数无效: %w", e)
	}
	m.cfg = cfg
	m.preview.QueueCount = len(m.queue)
	m.preview.BlacklistCount = len(m.cfg.Blacklist)
	if len(m.dailyCounts) > 0 && m.dailyPeriod == "" {
		m.preview.Warnings = append(m.preview.Warnings, "旧版每日计数缺少日期周期，不会写入新版以避免错误封禁")
	}
	if m.cfg.Command == "" {
		m.cfg.Command = "排队"
	}
	if len(m.cfg.Blacklist) > 5000 {
		return m, errors.New("too many blacklist entries")
	}
	return m, nil
}

func applyLegacySwitches(c *Config, flags map[string]any) {
	if c.Switches == nil {
		c.Switches = defaultSwitches()
	}
	s := c.Switches
	for _, item := range []struct {
		key  string
		dest *bool
	}{{"paidui", &s.Paidui}, {"guanfu_paidui", &s.Guanfu}, {"bfu_paidui", &s.Bfu}, {"chaoji_paidui", &s.Chaoji}, {"mifu_paidui", &s.Mifu}, {"quxiao_paidui", &s.Cancel}, {"xiugai_paidui", &s.Modify}, {"jianzhang_chadui", &s.GuardInsert}, {"fangguan_op", &s.RoomAdminOperator}} {
		if value, ok := flags[item.key]; ok {
			*item.dest = truth(value)
		}
	}
}

// Historic counts are stored as an array of "uid=count" pairs (or a mapping).
// IDs are conservatively attributed to Bilibili; unknown identities remain in
// original backup instead of being treated as trusted cross-platform IDs.
func parseLegacyDailyCounts(input any) map[string]int {
	out := map[string]int{}
	switch v := input.(type) {
	case string:
		var parsed any
		if json.Unmarshal([]byte(v), &parsed) == nil {
			return parseLegacyDailyCounts(parsed)
		}
	case []any:
		for _, x := range v {
			parts := strings.SplitN(str(x), "=", 2)
			if len(parts) != 2 {
				continue
			}
			n, err := strconv.Atoi(parts[1])
			if err != nil || n <= 0 || n > 99999 {
				continue
			}
			id := strings.TrimSpace(parts[0])
			if id == "" {
				continue
			}
			if _, err := strconv.ParseInt(id, 10, 64); err == nil {
				out["bilibili:"+id] = n
			}
		}
	case map[string]any:
		for id, value := range v {
			if _, err := strconv.ParseInt(id, 10, 64); err == nil {
				if count := number(value, 0); count > 0 && count < 100000 {
					out["bilibili:"+id] = count
				}
			}
		}
	}
	return out
}
