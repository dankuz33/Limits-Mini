package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const appName = "Limits Mini"
const appVersion = "0.2.0"

// No access or refresh tokens are ever stored in this configuration.
type Config struct {
	X           int32  `json:"x"`
	Y           int32  `json:"y"`
	Positioned  bool   `json:"positioned"`
	ShowWidget  bool   `json:"show_widget"`
	Topmost     bool   `json:"always_on_top"`
	Weekly      bool   `json:"show_weekly"`
	CodexWeekly bool   `json:"codex_show_weekly"`
	Remaining   bool   `json:"show_remaining"`
	Scale       int    `json:"scale_percent"`
	Interval    int    `json:"refresh_seconds"`
	ClaudeFile  string `json:"claude_credentials_file,omitempty"`
	CodexExe    string `json:"codex_executable,omitempty"`
}

func defaultConfig() Config {
	return Config{ShowWidget: true, Weekly: true, Scale: 100, Interval: 300}
}

func dataDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "LimitsMini", "data")
}

func loadConfig() Config {
	c := defaultConfig()
	b, err := os.ReadFile(filepath.Join(dataDir(), "settings.json"))
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Scale < 75 || c.Scale > 200 {
		c.Scale = 100
	}
	if c.Interval < 60 || c.Interval > 3600 {
		c.Interval = 300
	}
	return c
}

func saveConfig(c Config) error {
	if err := os.MkdirAll(dataDir(), 0700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// This file contains preferences and paths, never credentials or API responses.
	return os.WriteFile(filepath.Join(dataDir(), "settings.json"), b, 0600)
}

type Limit struct {
	Used    float64
	Reset   time.Time
	Minutes int
	Name    string
}

type Usage struct {
	Session *Limit
	Week    *Limit
	Extra   []Limit
	At      time.Time
	Source  string
	Error   string
	Stale   bool
	RetryAt time.Time
}

func (u Usage) HasData() bool { return u.Session != nil || u.Week != nil }
func (u Usage) Value(l *Limit, remaining bool) string {
	if l == nil || (!l.Reset.IsZero() && !time.Now().Before(l.Reset)) {
		return "--"
	}
	n := l.Used
	if remaining {
		n = 100 - n
	}
	n = math.Max(0, math.Min(100, n))
	return strconv.Itoa(int(math.Round(n)))
}

func limitLabel(l *Limit, fallback string) string {
	if l == nil || l.Minutes <= 0 {
		return fallback
	}
	if l.Minutes%1440 == 0 {
		return fmt.Sprintf("%d дн.", l.Minutes/1440)
	}
	if l.Minutes%60 == 0 {
		return fmt.Sprintf("%d ч", l.Minutes/60)
	}
	return fmt.Sprintf("%d мин", l.Minutes)
}
func resetText(t time.Time) string {
	if t.IsZero() {
		return "время сброса не получено"
	}
	d := time.Until(t)
	if d <= 0 {
		return "окно завершилось, ожидается обновление"
	}
	m := int(math.Ceil(d.Minutes()))
	if m >= 1440 {
		return fmt.Sprintf("сброс через %d дн. %d ч", m/1440, (m%1440)/60)
	}
	if m >= 60 {
		return fmt.Sprintf("сброс через %d ч %02d мин", m/60, m%60)
	}
	return fmt.Sprintf("сброс через %d мин", m)
}
func describeUsage(name string, u Usage, remaining bool) string {
	title := "Использовано"
	if remaining {
		title = "Осталось"
	}
	lines := []string{name + " · " + title}
	for i, l := range []*Limit{u.Session, u.Week} {
		if l == nil {
			continue
		}
		fallback := "Основное окно"
		if i == 1 {
			fallback = "Второе окно"
		}
		v := u.Value(l, remaining)
		if v != "--" {
			v += "%"
		}
		lines = append(lines, limitLabel(l, fallback)+": "+v+"; "+resetText(l.Reset))
	}
	for _, l := range u.Extra {
		v := u.Value(&l, remaining)
		if v != "--" {
			v += "%"
		}
		lines = append(lines, l.Name+": "+v)
	}
	if u.Error != "" {
		lines = append(lines, u.Error)
	}
	if u.Stale {
		lines = append(lines, "Показаны последние полученные данные, не актуальное измерение.")
	}
	if !u.At.IsZero() {
		lines = append(lines, "Обновлено: "+u.At.Local().Format("02.01 15:04:05"))
	}
	if u.Source != "" {
		lines = append(lines, "Источник: "+u.Source)
	}
	return strings.Join(lines, "\r\n")
}

func parseObject(b []byte) (map[string]interface{}, error) {
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v map[string]interface{}
	if err := d.Decode(&v); err != nil || v == nil {
		return nil, errors.New("некорректный JSON")
	}
	var tail interface{}
	if err := d.Decode(&tail); err != io.EOF {
		return nil, errors.New("лишние данные после JSON")
	}
	return v, nil
}
func obj(v interface{}) map[string]interface{} { m, _ := v.(map[string]interface{}); return m }
func number(v interface{}) (float64, bool) {
	var f float64
	switch n := v.(type) {
	case json.Number:
		v, e := n.Float64()
		if e != nil {
			return 0, false
		}
		f = v
	case float64:
		f = n
	case int:
		f = float64(n)
	default:
		return 0, false
	}
	return f, !math.IsNaN(f) && !math.IsInf(f, 0)
}
func text(v interface{}) string { s, _ := v.(string); return s }
func dateValue(v interface{}) time.Time {
	if s, ok := v.(string); ok {
		t, _ := time.Parse(time.RFC3339Nano, s)
		return t
	}
	if n, ok := number(v); ok && n > 0 && n < 32503680000 {
		return time.Unix(int64(n), 0)
	}
	return time.Time{}
}
func parseLimit(v interface{}, valueKey, resetKey string, minutes int) *Limit {
	m := obj(v)
	if m == nil {
		return nil
	}
	n, ok := number(m[valueKey])
	if !ok || n < 0 {
		return nil
	}
	return &Limit{Used: n, Reset: dateValue(m[resetKey]), Minutes: minutes}
}
func parseClaude(b []byte) (Usage, error) {
	m, err := parseObject(b)
	if err != nil {
		return Usage{}, err
	}
	u := Usage{Session: parseLimit(m["five_hour"], "utilization", "resets_at", 300),
		Week: parseLimit(m["seven_day"], "utilization", "resets_at", 10080)}
	// Optional legacy scoped limits are only reported when the backend returns a value.
	for _, k := range []string{"seven_day_opus", "seven_day_sonnet"} {
		if l := parseLimit(m[k], "utilization", "resets_at", 10080); l != nil {
			l.Name = strings.TrimPrefix(k, "seven_day_")
			u.Extra = append(u.Extra, *l)
		}
	}
	if !u.HasData() {
		return u, errors.New("в ответе нет доступных окон лимитов")
	}
	return u, nil
}
func parseCodexWindow(v interface{}) *Limit {
	m := obj(v)
	l := parseLimit(v, "usedPercent", "resetsAt", 0)
	if l != nil {
		if n, ok := number(m["windowDurationMins"]); ok {
			l.Minutes = int(n)
		}
	}
	return l
}
func parseCodex(b []byte) (Usage, error) {
	m, err := parseObject(b)
	if err != nil {
		return Usage{}, err
	}
	if r := obj(m["result"]); r != nil {
		m = r
	}
	r := obj(m["rateLimits"])
	if by := obj(m["rateLimitsByLimitId"]); by != nil {
		if codex := obj(by["codex"]); codex != nil {
			r = codex
		}
	}
	if r == nil || (text(r["limitId"]) != "" && text(r["limitId"]) != "codex") {
		return Usage{}, errors.New("основной лимит Codex не найден")
	}
	u := Usage{Session: parseCodexWindow(r["primary"]), Week: parseCodexWindow(r["secondary"])}
	if !u.HasData() {
		return u, errors.New("в ответе нет доступных окон лимитов Codex")
	}
	return u, nil
}
func parseCodexHTTP(b []byte) (Usage, error) {
	m, err := parseObject(b)
	if err != nil {
		return Usage{}, err
	}
	r := obj(m["rate_limit"])
	if r == nil {
		return Usage{}, errors.New("нет данных о лимите Codex")
	}
	get := func(k string) *Limit {
		v := obj(r[k])
		l := parseLimit(v, "used_percent", "reset_at", 0)
		if l != nil {
			if s, ok := number(v["limit_window_seconds"]); ok {
				l.Minutes = int(s / 60)
			}
		}
		return l
	}
	u := Usage{Session: get("primary_window"), Week: get("secondary_window")}
	if !u.HasData() {
		return u, errors.New("для этой авторизации лимит подписки не получен")
	}
	return u, nil
}
