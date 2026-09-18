package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClaudeParsing(t *testing.T) {
	cases := []struct {
		name, input               string
		session, week             float64
		hasSession, hasWeek, good bool
	}{
		{"both", `{"five_hour":{"utilization":46,"resets_at":"2030-01-01T00:00:00Z"},"seven_day":{"utilization":58}}`, 46, 58, true, true, true},
		{"zero_is_real", `{"five_hour":{"utilization":0}}`, 0, 0, true, false, true},
		{"fraction", `{"five_hour":{"utilization":46.5}}`, 46.5, 0, true, false, true},
		{"weekly_only", `{"seven_day":{"utilization":12}}`, 0, 12, false, true, true},
		{"null_is_missing", `{"five_hour":{"utilization":null}}`, 0, 0, false, false, false},
		{"field_missing", `{"five_hour":{"resets_at":0}}`, 0, 0, false, false, false},
		{"empty", `{}`, 0, 0, false, false, false},
		{"no_object", `null`, 0, 0, false, false, false},
		{"bad_json", `<html>error</html>`, 0, 0, false, false, false},
		{"negative", `{"five_hour":{"utilization":-1}}`, 0, 0, false, false, false},
		{"string_not_number", `{"five_hour":{"utilization":"34"}}`, 0, 0, false, false, false},
		{"utf8_bom", "\xef\xbb\xbf" + `{"five_hour":{"utilization":8}}`, 8, 0, true, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, e := parseClaude([]byte(c.input))
			if (e == nil) != c.good {
				t.Fatalf("success=%v, wanted %v", e == nil, c.good)
			}
			if (u.Session != nil) != c.hasSession || (u.Week != nil) != c.hasWeek {
				t.Fatal("missing windows were not preserved")
			}
			if u.Session != nil && u.Session.Used != c.session {
				t.Fatal("incorrect primary")
			}
			if u.Week != nil && u.Week.Used != c.week {
				t.Fatal("incorrect weekly")
			}
		})
	}
}
func TestCodexParsing(t *testing.T) {
	cases := []struct {
		name, input string
		value       float64
		good        bool
	}{
		{"legacy", `{"rateLimits":{"primary":{"usedPercent":43,"windowDurationMins":300,"resetsAt":2000000000},"secondary":{"usedPercent":31,"windowDurationMins":10080}}}`, 43, true},
		{"multi_bucket_prefer_codex", `{"rateLimits":{"limitId":"other","primary":{"usedPercent":99}},"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":18}}}}`, 18, true},
		{"do_not_mislabel_other", `{"rateLimits":{"limitId":"other","primary":{"usedPercent":99}}}`, 0, false},
		{"only_multi", `{"rateLimitsByLimitId":{"codex":{"primary":{"usedPercent":10}}}}`, 10, true},
		{"full_rpc", `{"id":1,"result":{"rateLimits":{"primary":{"usedPercent":0}}}}`, 0, true},
		{"null_primary", `{"rateLimits":{"primary":{"usedPercent":null}}}`, 0, false},
		{"empty", `{"rateLimits":null}`, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, e := parseCodex([]byte(c.input))
			if (e == nil) != c.good {
				t.Fatalf("error=%v", e)
			}
			if c.good && (u.Session == nil || u.Session.Used != c.value) {
				t.Fatal("wrong value")
			}
		})
	}
}
func TestCodexHTTPParsing(t *testing.T) {
	u, e := parseCodexHTTP([]byte(`{"rate_limit":{"primary_window":{"used_percent":43,"limit_window_seconds":18000,"reset_at":2000000000},"secondary_window":{"used_percent":25,"limit_window_seconds":604800}}}`))
	if e != nil || u.Session.Used != 43 || u.Session.Minutes != 300 || u.Week.Minutes != 10080 {
		t.Fatalf("%+v %v", u, e)
	}
	if _, e = parseCodexHTTP([]byte(`{"rate_limit":{"primary_window":{"used_percent":null}}}`)); e == nil {
		t.Fatal("null incorrectly shown as zero")
	}
}
func TestDisplayFormatting(t *testing.T) {
	u := Usage{}
	cases := []struct {
		name      string
		l         *Limit
		remaining bool
		want      string
	}{
		{"missing", nil, false, "--"}, {"zero", &Limit{Used: 0}, false, "0"},
		{"round", &Limit{Used: 46.5}, false, "47"}, {"remaining", &Limit{Used: 46}, true, "54"},
		{"over_cap", &Limit{Used: 109}, false, "100"}, {"remaining_floor", &Limit{Used: 109}, true, "0"},
		{"expired_not_zero", &Limit{Used: 80, Reset: time.Now().Add(-time.Second)}, false, "--"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := u.Value(c.l, c.remaining); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
	if limitLabel(&Limit{Minutes: 15}, "") != "15 мин" {
		t.Fatal("codex duration falsely assumed 5h")
	}
	if limitLabel(&Limit{Minutes: 10080}, "") != "7 дн." {
		t.Fatal("week label")
	}
}
func TestConfiguration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	c := defaultConfig()
	c.X = 500
	c.ShowWidget = false
	c.ClaudeFile = `C:\Users\Test\.claude\.credentials.json`
	if e := saveConfig(c); e != nil {
		t.Fatal(e)
	}
	got := loadConfig()
	if got.X != 500 || got.ShowWidget || got.ClaudeFile != c.ClaudeFile {
		t.Fatal("preferences not preserved")
	}
	b, _ := os.ReadFile(filepath.Join(dataDir(), "settings.json"))
	if strings.Contains(string(b), "accessToken") || strings.Contains(string(b), "refresh_token") {
		t.Fatal("secret field in settings")
	}
	_ = os.WriteFile(filepath.Join(dataDir(), "settings.json"), []byte(`{"scale_percent":0,"refresh_seconds":1}`), 0600)
	got = loadConfig()
	if got.Scale != 100 || got.Interval != 300 {
		t.Fatal("invalid preferences not normalized")
	}
}
func TestCredentialPaths(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "custom"))
	if !strings.HasSuffix(claudeCredentialPath(Config{}), filepath.Join("custom", ".credentials.json")) {
		t.Fatal("CLAUDE_CONFIG_DIR not honored")
	}
	if claudeCredentialPath(Config{ClaudeFile: "custom.json"}) != "custom.json" {
		t.Fatal("override")
	}
	t.Setenv("CODEX_HOME", "test-codex-home")
	if codexHome() != "test-codex-home" {
		t.Fatal("CODEX_HOME")
	}
}

type mockTransport func(*http.Request) (*http.Response, error)

func (m mockTransport) RoundTrip(r *http.Request) (*http.Response, error) { return m(r) }
func mockHTTP(t *testing.T, fn mockTransport) {
	t.Helper()
	old := httpClient
	httpClient = newHTTPClient()
	httpClient.Transport = fn
	t.Cleanup(func() { httpClient = old })
}
func response(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestHTTPGuards(t *testing.T) {
	calls := 0
	mockHTTP(t, func(r *http.Request) (*http.Response, error) { calls++; return response(200, `{}`), nil })
	if _, e := requestUsage(context.Background(), "https://example.com/steal", "fake-token", ""); e == nil {
		t.Fatal("arbitrary domain accepted")
	}
	if calls != 0 {
		t.Fatal("request sent to arbitrary host")
	}
	if httpClient.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirects allowed")
	}
}
func TestHTTPHeaders(t *testing.T) {
	mockHTTP(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "api.anthropic.com" || r.Method != "GET" || r.Header.Get("Authorization") != "Bearer test-only" || r.Header.Get("anthropic-beta") == "" {
			t.Fatal("bad request")
		}
		return response(200, `{"five_hour":{"utilization":4}}`), nil
	})
	if _, e := requestUsage(context.Background(), claudeUsageURL, "test-only", ""); e != nil {
		t.Fatal(e)
	}
}
func TestHTTPErrorCases(t *testing.T) {
	for _, code := range []int{301, 401, 403, 429, 500} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			mockHTTP(t, func(r *http.Request) (*http.Response, error) {
				v := response(code, `{"secret":"NEVER-ECHO"}`)
				v.Header.Set("Retry-After", "120")
				return v, nil
			})
			_, e := requestUsage(context.Background(), claudeUsageURL, "FAKE-SECRET", "")
			if e == nil {
				t.Fatal("missing error")
			}
			if strings.Contains(e.Error(), "SECRET") || strings.Contains(e.Error(), "NEVER-ECHO") {
				t.Fatal("secret leak")
			}
			if code == 429 {
				fe, ok := e.(*fetchError)
				if !ok || time.Until(fe.retry) < 119*time.Second {
					t.Fatal("no backoff")
				}
			}
		})
	}
}
func TestHTTPNetworkErrorRedaction(t *testing.T) {
	mockHTTP(t, func(r *http.Request) (*http.Response, error) { return nil, errors.New("raw error with FAKE-SECRET") })
	_, e := requestUsage(context.Background(), claudeUsageURL, "FAKE-SECRET", "")
	if e == nil || strings.Contains(e.Error(), "FAKE-SECRET") {
		t.Fatal("network error not sanitized")
	}
}
func TestClaudeCredentialsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	original := []byte(`{"claudeAiOauth":{"accessToken":"TEST-ACCESS","refreshToken":"TEST-REFRESH","expiresAt":2200000000000}}`)
	_ = os.WriteFile(path, original, 0600)
	mockHTTP(t, func(r *http.Request) (*http.Response, error) {
		return response(200, `{"five_hour":{"utilization":46},"seven_day":{"utilization":58}}`), nil
	})
	u, e := fetchClaude(context.Background(), Config{ClaudeFile: path})
	if e != nil || u.Session.Used != 46 {
		t.Fatalf("%+v %v", u, e)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("credentials changed")
	}
}
func TestExpiredClaudeNoRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	original := []byte(`{"claudeAiOauth":{"accessToken":"TEST-ACCESS","refreshToken":"TEST-REFRESH","expiresAt":1000}}`)
	_ = os.WriteFile(path, original, 0600)
	calls := 0
	mockHTTP(t, func(r *http.Request) (*http.Response, error) { calls++; return response(200, `{}`), nil })
	_, e := fetchClaude(context.Background(), Config{ClaudeFile: path})
	if e == nil || calls != 0 {
		t.Fatal("expired token was used or refreshed")
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("credentials changed")
	}
}
func TestRetryBounds(t *testing.T) {
	for _, v := range []string{"", "-5", "1", "120", "999999999", "invalid", time.Now().Add(2 * time.Minute).UTC().Format(http.TimeFormat)} {
		d := time.Until(retryTime(v))
		if d < 59*time.Second || d > time.Hour {
			t.Fatalf("unsafe retry delay %s", d)
		}
	}
}
func TestValidJSONRejectsNonObjects(t *testing.T) {
	for _, s := range []string{`[]`, `"string"`, `null`, `123`, `{`} {
		if _, e := parseObject([]byte(s)); e == nil {
			t.Fatalf("accepted %s", s)
		}
	}
	m, _ := parseObject([]byte(`{"value":1}`))
	if _, ok := m["value"].(json.Number); !ok {
		t.Fatal("number fidelity")
	}
}
