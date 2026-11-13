package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CheckConfiguration validates a private temporary file without starting a connection.
func CheckConfiguration(ctx context.Context, core string, config map[string]any) error {
	temporary, err := os.MkdirTemp("", "miniproxy-validate-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temporary)
	path := filepath.Join(temporary, "config.json")
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}
	if core == "" {
		return errors.New("找不到内核，无法检查配置")
	}
	output, err := exec.CommandContext(ctx, core, "check", "-c", path, "--disable-color").CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("内核配置检查失败：%s", strings.TrimSpace(string(output)))
	}
	return nil
}
