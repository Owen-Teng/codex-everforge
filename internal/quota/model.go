package quota

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"
)

type RawWindow struct {
	UsedPercent *float64 `json:"usedPercent"`
	Duration    *int64   `json:"windowDurationMins"`
	Reset       *int64   `json:"resetsAt"`
}
type RawBucket struct {
	ID        *string    `json:"limitId"`
	Name      *string    `json:"limitName"`
	Primary   *RawWindow `json:"primary"`
	Secondary *RawWindow `json:"secondary"`
}
type Response struct {
	AccountID    *string               `json:"accountId"`
	Legacy       *RawBucket            `json:"rateLimits"`
	Buckets      map[string]*RawBucket `json:"rateLimitsByLimitId"`
	ResetCredits *struct {
		AvailableCount *int `json:"availableCount"`
	} `json:"rateLimitResetCredits"`
}
type Window struct {
	Name      string `json:"name"`
	Remaining int    `json:"remaining"`
	Duration  *int64 `json:"durationMins"`
	Reset     *int64 `json:"resetsAt"`
}
type Choice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Snapshot struct {
	State               string   `json:"state"`
	Remaining           *int     `json:"remaining"`
	Color               string   `json:"color"`
	Bucket              string   `json:"bucket"`
	BucketName          string   `json:"bucketName"`
	Scope               string   `json:"scope"`
	Windows             []Window `json:"windows"`
	Choices             []Choice `json:"choices"`
	Message             string   `json:"message"`
	UpdatedAt           int64    `json:"updatedAt"`
	LastSuccess         int64    `json:"lastSuccess"`
	NextRefresh         int64    `json:"nextRefresh"`
	Interval            int      `json:"intervalSeconds"`
	Stale               bool     `json:"stale"`
	Partial             bool     `json:"partial"`
	Demo                bool     `json:"demo"`
	ConfigWarning       string   `json:"configWarning"`
	AccountKey          string   `json:"-"`
	AvailableResetCount *int     `json:"availableResetCount,omitempty"`
}
type Failure struct {
	Kind       string
	RetryAfter time.Duration
}

func (e *Failure) Error() string { return e.Kind }
func Fail(kind string) error     { return &Failure{Kind: kind} }
func Reason(err error) string {
	kind := "unknown"
	if e, ok := err.(*Failure); ok {
		kind = e.Kind
	}
	switch kind {
	case "not_installed":
		return "未找到 Codex CLI。请安装并登录，或在右键菜单选择 codex.exe。"
	case "not_logged_in":
		return "请先在 Codex CLI 中登录 ChatGPT 账号，再刷新。"
	case "unsupported_account":
		return "当前登录方式不提供 ChatGPT 套餐额度；API 余额与套餐额度不同。"
	case "auth":
		return "Codex 登录已失效。请在 Codex 中重新登录后刷新。"
	case "timeout":
		return "读取超时，将自动重试。"
	case "network":
		return "暂时无法连接额度服务，将自动重试。"
	case "rate_limited":
		return "额度服务暂时限流，已延后刷新。"
	case "unsupported_version":
		return "当前 Codex 版本不支持额度接口，请升级 Codex CLI。"
	case "select_bucket":
		return "请在右键菜单中选择要显示的额度桶。"
	case "bucket_missing":
		return "所选额度桶已不在响应中，请重新选择。"
	case "invalid_data":
		return "额度字段不完整或超出范围，暂不显示百分比。"
	case "no_windows":
		return "当前没有可用的套餐限制窗口；不等于额度为 100%。"
	case "account_changed":
		return "读取期间账号发生变化，已清除旧值，请刷新。"
	case "cancelled":
		return "读取已取消。"
	default:
		return "暂时无法读取额度，请稍后刷新或检查 Codex CLI 登录状态。"
	}
}
func Tone(p *int) string {
	if p == nil {
		return "gray"
	}
	if *p >= 60 {
		return "green"
	}
	if *p >= 20 {
		return "yellow"
	}
	return "red"
}
func SafeLabel(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	rs := []rune(strings.TrimSpace(s))
	if len(rs) > 64 {
		rs = rs[:64]
	}
	return string(rs)
}
func DurationName(m *int64) string {
	if m == nil || *m <= 0 {
		return "时长未提供"
	}
	if *m%1440 == 0 {
		return fmt.Sprintf("%d 天", *m/1440)
	}
	if *m%60 == 0 {
		return fmt.Sprintf("%d 小时", *m/60)
	}
	return fmt.Sprintf("%d 分钟", *m)
}
func Normalize(raw Response, selected string) (Snapshot, error) {
	s := Snapshot{State: "error", Color: "gray"}
	if raw.ResetCredits != nil && raw.ResetCredits.AvailableCount != nil && *raw.ResetCredits.AvailableCount >= 0 {
		value := *raw.ResetCredits.AvailableCount
		s.AvailableResetCount = &value
	}
	buckets := raw.Buckets
	if buckets == nil && raw.Legacy != nil {
		id := "legacy"
		if raw.Legacy.ID != nil && *raw.Legacy.ID != "" {
			id = *raw.Legacy.ID
		}
		buckets = map[string]*RawBucket{id: raw.Legacy}
	}
	ids := make([]string, 0, len(buckets))
	for id, b := range buckets {
		if b != nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		name := SafeLabel(id)
		if buckets[id].Name != nil && SafeLabel(*buckets[id].Name) != "" {
			name = SafeLabel(*buckets[id].Name)
		}
		if id == "codex" {
			name = "Codex"
		}
		s.Choices = append(s.Choices, Choice{id, name})
	}
	if selected == "" {
		if _, ok := buckets["codex"]; ok {
			selected = "codex"
		} else {
			return s, Fail("select_bucket")
		}
	}
	b, ok := buckets[selected]
	if !ok || b == nil {
		return s, Fail("bucket_missing")
	}
	s.Bucket = selected
	for _, c := range s.Choices {
		if c.ID == selected {
			s.BucketName = c.Name
		}
	}
	if b.ID != nil && *b.ID != "" && *b.ID != selected {
		return s, Fail("invalid_data")
	}
	invalid := false
	for i, w := range []*RawWindow{b.Primary, b.Secondary} {
		if w == nil {
			continue
		}
		if w.UsedPercent == nil || math.IsNaN(*w.UsedPercent) || math.IsInf(*w.UsedPercent, 0) || *w.UsedPercent < 0 || *w.UsedPercent > 100 {
			invalid = true
			continue
		}
		if (w.Duration != nil && *w.Duration <= 0) || (w.Reset != nil && (*w.Reset <= 0 || *w.Reset > 253402300799)) {
			invalid = true
			continue
		}
		value := int(math.Floor(100 - *w.UsedPercent))
		name := DurationName(w.Duration) + "窗口"
		if w.Duration == nil {
			if i == 0 {
				name = "主限制窗口（时长未提供）"
			} else {
				name = "次限制窗口（时长未提供）"
			}
		}
		s.Windows = append(s.Windows, Window{name, value, w.Duration, w.Reset})
		if s.Remaining == nil || value < *s.Remaining {
			v := value
			s.Remaining = &v
			s.Scope = name
		}
		if w.Duration == nil || w.Reset == nil {
			s.Partial = true
		}
	}
	if invalid {
		s.Remaining = nil
		s.Partial = true
		return s, Fail("invalid_data")
	}
	if s.Remaining == nil {
		return s, Fail("no_windows")
	}
	s.State = "ready"
	s.Color = Tone(s.Remaining)
	s.Message = "所选额度桶内，显示剩余最少的有效限制窗口。"
	if s.Partial {
		s.Message = "部分窗口信息未提供；已按返回的有效比例显示。"
	}
	return s, nil
}
