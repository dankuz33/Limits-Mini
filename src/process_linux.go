package main

import (
	"context"
	"os/exec"
)

func makeCodexCommand(ctx context.Context, path string) (*exec.Cmd, error) {
	return exec.CommandContext(ctx, path, "app-server"), nil
}
func containProcess(cmd *exec.Cmd) func() { return func() {} }
