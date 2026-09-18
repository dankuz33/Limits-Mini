package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalCodexRPCHandshake(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	script := `#!/bin/sh
IFS= read -r first
case "$first" in *'"initialize"'*) ;; *) exit 4;; esac
printf '%s\n' '{"id":0,"result":{"userAgent":"fixture"}}'
IFS= read -r second
case "$second" in *'"initialized"'*) ;; *) exit 5;; esac
IFS= read -r third
case "$third" in *'"account/rateLimits/read"'*) ;; *) exit 6;; esac
printf '%s\n' '{"id":1,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":43,"windowDurationMins":300,"resetsAt":2000000000},"secondary":{"usedPercent":31,"windowDurationMins":10080}}}}'
`
	path := filepath.Join(dir, "fixture-codex")
	if e := os.WriteFile(path, []byte(script), 0700); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	u, e := readCodexRPC(ctx, path)
	if e != nil {
		t.Fatal(e)
	}
	if u.Session.Used != 43 || u.Week.Used != 31 {
		t.Fatal("bad values")
	}
}
func TestCodexRPCDeadProcess(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	p := filepath.Join(dir, "codex-exit")
	_ = os.WriteFile(p, []byte("#!/bin/sh\nexit 1\n"), 0700)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e := readCodexRPC(ctx, p); e == nil {
		t.Fatal("dead process accepted")
	}
}
