//go:build windows

package settings

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const startupRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`

func SyncStartup(exePath string, enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, startupRegistryPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("无法打开 Windows 启动设置: %w", err)
	}
	defer key.Close()
	if !enabled {
		if err := key.DeleteValue("TouchDict"); err != nil && err != registry.ErrNotExist {
			return fmt.Errorf("无法关闭开机自启动: %w", err)
		}
		return nil
	}
	command := `"` + strings.ReplaceAll(exePath, `"`, `\"`) + `"`
	if err := key.SetStringValue("TouchDict", command); err != nil {
		return fmt.Errorf("无法启用开机自启动: %w", err)
	}
	return nil
}
