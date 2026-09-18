package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"
const codexUsageURL = "https://chatgpt.com/backend-api/wham/usage"

// Never follow redirects with account credentials. TLS certificate validation stays enabled.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 18 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
}

var httpClient = newHTTPClient()

type fetchError struct {
	message string
	retry   time.Time
}

func (e *fetchError) Error() string { return e.message }

func retryTime(value string) time.Time {
	delay := 10 * time.Minute
	if seconds, e := strconv.Atoi(value); e == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	} else if t, e := http.ParseTime(value); e == nil && t.After(time.Now()) {
		delay = time.Until(t)
	}
	if delay < time.Minute {
		delay = time.Minute
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return time.Now().Add(delay)
}
func requestUsage(ctx context.Context, url, token, account string) ([]byte, error) {
	// Endpoints are hard-coded. A credentials file cannot redirect this request to another host.
	if url != claudeUsageURL && url != codexUsageURL {
		return nil, errors.New("недопустимый адрес API")
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LimitsMini/"+appVersion)
	if url == claudeUsageURL {
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		req.Header.Set("anthropic-version", "2023-06-01")
	}
	if account != "" {
		req.Header.Set("ChatGPT-Account-Id", account)
	}
	res, e := httpClient.Do(req)
	if e != nil {
		return nil, errors.New("Нет соединения с сервисом. Проверьте сеть или VPN.")
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case 200:
	case 401:
		return nil, errors.New("Вход истёк. Откройте CLI и войдите заново.")
	case 403:
		return nil, errors.New("Доступ отклонён (403). Проверьте вход, подписку и доступность сервиса.")
	case 429:
		return nil, &fetchError{"Сервис ограничил частоту запросов (429). Сделана пауза.", retryTime(res.Header.Get("Retry-After"))}
	default:
		return nil, fmt.Errorf("Сервис ответил HTTP %d. Повторим позже.", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, 2*1024*1024+1))
	if e != nil {
		return nil, errors.New("Не удалось прочитать ответ сервиса.")
	}
	if len(b) > 2*1024*1024 {
		return nil, errors.New("Ответ сервиса слишком большой.")
	}
	return b, nil
}
func readSmallJSON(path string) (map[string]interface{}, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 1024*1024 {
		return nil, errors.New("файл слишком большой")
	}
	return parseObject(b)
}
func claudeCredentialCandidates(c Config) []string {
	if c.ClaudeFile != "" {
		return []string{c.ClaudeFile}
	}
	h, _ := os.UserHomeDir()
	var raw []string
	// Claude Code can separate its general config directory and secure-storage directory.
	// On Windows both stores use a .credentials.json file, so check the secure override first.
	if s := os.Getenv("CLAUDE_SECURESTORAGE_CONFIG_DIR"); s != "" {
		raw = append(raw, filepath.Join(s, ".credentials.json"))
	}
	if s := os.Getenv("CLAUDE_CONFIG_DIR"); s != "" {
		raw = append(raw, filepath.Join(s, ".credentials.json"))
	}
	raw = append(raw,
		filepath.Join(h, ".claude", ".credentials.json"),
		filepath.Join(h, ".claude", "credentials.json"), // compatibility with older/community layouts
	)
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if v == "" {
			continue
		}
		k := strings.ToLower(filepath.Clean(v))
		if !seen[k] {
			seen[k] = true
			out = append(out, v)
		}
	}
	return out
}
func claudeCredentialPath(c Config) string {
	for _, p := range claudeCredentialCandidates(c) {
		if fileExists(p) {
			return p
		}
	}
	cands := claudeCredentialCandidates(c)
	if len(cands) > 0 {
		return cands[0]
	}
	return ""
}
func readClaudeCredentials(c Config) (map[string]interface{}, string, error) {
	foundFile := false
	for _, p := range claudeCredentialCandidates(c) {
		m, e := readSmallJSON(p)
		if e != nil {
			continue
		}
		foundFile = true
		oauth := obj(m["claudeAiOauth"])
		if oauth != nil && text(oauth["accessToken"]) != "" {
			return m, p, nil
		}
	}
	if foundFile {
		return nil, "", errors.New("Файл Claude найден, но вход Claude Code по подписке в нём отсутствует.")
	}
	return nil, "", errors.New("Вход Claude Desktop не передаётся Claude Code. Подключите Claude Code один раз через ПКМ → Подключение.")
}
func codexHome() string {
	if s := os.Getenv("CODEX_HOME"); s != "" {
		return s
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".codex")
}
func fetchClaude(ctx context.Context, c Config) (Usage, error) {
	m, sourcePath, e := readClaudeCredentials(c)
	if e != nil {
		return Usage{}, e
	}
	oauth := obj(m["claudeAiOauth"])
	token := text(oauth["accessToken"])
	if token == "" {
		return Usage{}, errors.New("Нужен вход Claude Code по подписке. API-ключ не подходит.")
	}
	// Read-only: do NOT refresh or overwrite the CLI credentials.
	if exp, ok := number(oauth["expiresAt"]); ok && exp > 0 && float64(time.Now().UnixMilli()) >= exp {
		return Usage{}, errors.New("Токен Claude истёк. Откройте Claude Code; при необходимости выполните /login.")
	}
	b, e := requestUsage(ctx, claudeUsageURL, token, "")
	if e != nil {
		return Usage{}, e
	}
	u, e := parseClaude(b)
	if e != nil {
		return Usage{}, errors.New("Claude не вернул окна лимитов. Проверьте тип входа или попробуйте позже.")
	}
	u.At = time.Now()
	u.Source = "Claude usage API · Claude Code OAuth (только чтение) · " + filepath.Base(filepath.Dir(sourcePath))
	return u, nil
}

func findCodex(c Config) string {
	if c.CodexExe != "" {
		if fileExists(c.CodexExe) {
			return c.CodexExe
		}
		return ""
	}
	h, _ := os.UserHomeDir()
	// Prefer native executables: they are smaller and can be terminated cleanly.
	roots := []string{
		filepath.Join(os.Getenv("APPDATA"), "npm", "node_modules", "@openai", "codex"),
		filepath.Join(os.Getenv("ProgramFiles"), "nodejs", "node_modules", "@openai", "codex"),
	}
	patterns := []string{
		filepath.Join(h, ".local", "bin", "codex.exe"),
		filepath.Join(h, ".cargo", "bin", "codex.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "codex.exe"),
		filepath.Join(h, ".vscode", "extensions", "openai.chatgpt-*", "bin", "windows-*", "codex.exe"),
		filepath.Join(h, ".vscode-insiders", "extensions", "openai.chatgpt-*", "bin", "windows-*", "codex.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Codex", "resources", "codex.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Codex", "resources", "app.asar.unpacked", "codex.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "WindowsApps", "OpenAI.Codex_*", "app", "resources", "codex.exe"),
	}
	if p, e := exec.LookPath("codex.exe"); e == nil {
		return p
	}
	for _, r := range roots {
		patterns = append(patterns, filepath.Join(r, "vendor", "*-pc-windows-msvc", "codex", "codex.exe"),
			filepath.Join(r, "node_modules", "@openai", "codex-win32-*", "vendor", "*-pc-windows-msvc", "codex", "codex.exe"),
			filepath.Join(filepath.Dir(r), "codex-win32-*", "vendor", "*-pc-windows-msvc", "codex", "codex.exe"))
	}
	for _, pat := range patterns {
		found, _ := filepath.Glob(pat)
		for i := len(found) - 1; i >= 0; i-- {
			if fileExists(found[i]) {
				return found[i]
			}
		}
	}
	// The npm shim is allowed only through cmd.exe with a safe absolute path.
	if p, e := exec.LookPath("codex.cmd"); e == nil {
		return p
	}
	return ""
}
func fileExists(p string) bool { s, e := os.Stat(p); return e == nil && !s.IsDir() }

type rpcMessage struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func readCodexRPC(ctx context.Context, path string) (Usage, error) {
	cmd, e := makeCodexCommand(ctx, path)
	if e != nil {
		return Usage{}, e
	}
	// Run outside any project: no project content, prompts or repository inspection.
	cmd.Dir = codexHome()
	if !fileDirExists(cmd.Dir) {
		cmd.Dir, _ = os.UserHomeDir()
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		return Usage{}, errors.New("Не удалось открыть канал Codex.")
	}
	in, e := cmd.StdinPipe()
	if e != nil {
		return Usage{}, errors.New("Не удалось открыть канал Codex.")
	}
	cmd.Stderr = nil // OS null device: never copy account details to app logs.
	if e = cmd.Start(); e != nil {
		return Usage{}, errors.New("Не удалось запустить локальный Codex.")
	}
	release := containProcess(cmd)
	var once sync.Once
	cleanup := func() { once.Do(release) }
	defer cleanup()
	defer func() {
		_ = in.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(600 * time.Millisecond):
			cleanup()
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	replies := make(chan rpcMessage, 8)
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		s := bufio.NewScanner(out)
		s.Buffer(make([]byte, 4096), 2*1024*1024)
		for s.Scan() {
			var r rpcMessage
			if json.Unmarshal(s.Bytes(), &r) != nil || r.ID == nil {
				continue
			}
			select {
			case replies <- r:
			case <-ctx.Done():
				return
			}
		}
	}()
	send := func(v interface{}) error { return json.NewEncoder(in).Encode(v) }
	wait := func(id int) (json.RawMessage, error) {
		for {
			select {
			case <-ctx.Done():
				return nil, errors.New("Codex не ответил вовремя. Проверьте вход и сеть.")
			case <-scanDone:
				// A process can close stdout immediately after a valid final reply.
				select {
				case r := <-replies:
					if r.ID != nil && *r.ID == id && len(r.Result) > 0 {
						return r.Result, nil
					}
				default:
				}
				return nil, errors.New("Codex закрыл канал. Проверьте версию CLI и вход.")
			case r := <-replies:
				if r.ID == nil || *r.ID != id {
					continue
				}
				if len(r.Error) > 0 && string(r.Error) != "null" {
					return nil, errors.New("Codex не вернул лимиты. Войдите через codex login по подписке ChatGPT.")
				}
				return r.Result, nil
			}
		}
	}
	if e = send(map[string]interface{}{"method": "initialize", "id": 0, "params": map[string]interface{}{
		"clientInfo": map[string]string{"name": "limits_mini", "title": appName, "version": appVersion},
	}}); e != nil {
		return Usage{}, errors.New("Не удалось отправить запрос Codex.")
	}
	if _, e = wait(0); e != nil {
		return Usage{}, e
	}
	_ = send(map[string]interface{}{"method": "initialized", "params": map[string]interface{}{}})
	if e = send(map[string]interface{}{"method": "account/rateLimits/read", "id": 1}); e != nil {
		return Usage{}, errors.New("Не удалось запросить лимиты Codex.")
	}
	b, e := wait(1)
	if e != nil {
		return Usage{}, e
	}
	u, e := parseCodex(b)
	if e != nil {
		return Usage{}, errors.New("Codex не вернул основной лимит подписки.")
	}
	u.At = time.Now()
	u.Source = "Локальный Codex app-server · account/rateLimits/read"
	return u, nil
}
func fileDirExists(p string) bool { s, e := os.Stat(p); return e == nil && s.IsDir() }

func fetchCodex(ctx context.Context, c Config) (Usage, error) {
	path := findCodex(c)
	var rpcErr error
	if path != "" {
		rpcCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		u, e := readCodexRPC(rpcCtx, path)
		cancel()
		if e == nil {
			return u, nil
		}
		rpcErr = e
	}
	// Fallback for a local auth.json when CLI is unavailable. Still read-only.
	m, e := readSmallJSON(filepath.Join(codexHome(), "auth.json"))
	if e != nil {
		if rpcErr != nil {
			return Usage{}, rpcErr
		}
		return Usage{}, errors.New("Не найден вход Codex. Выполните codex login или укажите codex.exe в меню.")
	}
	tokens := obj(m["tokens"])
	at := text(tokens["access_token"])
	account := text(tokens["account_id"])
	if at == "" {
		if rpcErr != nil {
			return Usage{}, rpcErr
		}
		return Usage{}, errors.New("Нужен вход Codex по подписке ChatGPT. API-ключ не подходит.")
	}
	b, e := requestUsage(ctx, codexUsageURL, at, account)
	if e != nil {
		return Usage{}, e
	}
	u, e := parseCodexHTTP(b)
	if e != nil {
		return Usage{}, errors.New("Не удалось прочитать лимиты Codex. Укажите актуальный codex.exe.")
	}
	u.At = time.Now()
	u.Source = "Codex usage API · локальный auth.json (только чтение)"
	return u, nil
}
