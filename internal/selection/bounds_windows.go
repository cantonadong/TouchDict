//go:build windows

package selection

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lxn/win"
	"os"
	"os/exec"
	"syscall"
	"time"
	"touchdict/internal/model"
)

// Isolate provider hangs and crashes. Use native COM to bypass the .NET
// RawTextRange_GetText access violation recorded in Windows event logs.
func readSelectionDetails(parent context.Context, selectedText string, pointer win.POINT) (*model.SelectionBounds, string, string) {
	ctx, cancel := context.WithTimeout(parent, 4*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		return nil, "", "context executable: " + err.Error()
	}
	cmd := exec.CommandContext(ctx, executable, "--read-selection-context")
	cmd.Env = append(os.Environ(), "TOUCHDICT_SELECTION="+selectedText, fmt.Sprintf("TOUCHDICT_POINTER_X=%d", pointer.X), fmt.Sprintf("TOUCHDICT_POINTER_Y=%d", pointer.Y))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", "native context reader: " + ctx.Err().Error()
		}
		return nil, "", "native context reader failed: " + err.Error()
	}
	var details contextDetails
	if json.Unmarshal(output, &details) != nil {
		return nil, "", "native context reader returned invalid JSON"
	}
	if details.Right <= details.Left || details.Bottom <= details.Top {
		return nil, normalize(details.Context), details.Diagnostic
	}
	return &details.SelectionBounds, normalize(details.Context), details.Diagnostic
}
