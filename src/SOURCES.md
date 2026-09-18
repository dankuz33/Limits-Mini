# Limits Mini 0.2.0 implementation notes

Standalone Go + Win32 utility. No third-party Go modules are required.
The visual icons are locally generated high-resolution assets inspired by the supplied reference.
No font files are bundled; Windows uses Segoe UI Variable Display (with system fallback).

Provider integrations:
- Claude Code credential storage: `%USERPROFILE%\.claude\.credentials.json`, plus
  `CLAUDE_CONFIG_DIR` / `CLAUDE_SECURESTORAGE_CONFIG_DIR` overrides.
- Claude usage: `https://api.anthropic.com/api/oauth/usage`.
- Codex: local `codex app-server`, `account/rateLimits/read`.
- Codex fallback: local `~/.codex/auth.json` with the ChatGPT usage endpoint.

The application intentionally does not read Claude Desktop Electron cookies/token storage.
Desktop and Claude Code logins are treated as separate security boundaries.

Build:
`GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H=windowsgui" -o LimitsMini.exe .`

`resource_windows_amd64.syso` is generated from `generate_resource.py` and contains
only Windows resources (icon + manifest), not executable application logic.
