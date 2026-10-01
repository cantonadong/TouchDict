//go:build windows

package selection

import (
	"context"
	"encoding/json"
	"os/exec"
	"syscall"
	"time"
	"touchdict/internal/model"
)

// UI Automation returns visible selection rectangles in screen pixels.
// Run on a separate hidden STA process so a stalled provider cannot block
// the app's UI thread or indefinitely delay clipboard-based lookup.
func readSelectionBounds(parent context.Context) *model.SelectionBounds {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	const script = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$element = [System.Windows.Automation.AutomationElement]::FocusedElement
$walker = [System.Windows.Automation.TreeWalker]::ControlViewWalker
for ($attempt = 0; $attempt -lt 8 -and $null -ne $element; $attempt++) {
    $pattern = $null
    if ($element.TryGetCurrentPattern([System.Windows.Automation.TextPattern]::Pattern, [ref]$pattern)) {
        $rectangles = @()
        foreach ($range in $pattern.GetSelection()) {
            $rectangles += @($range.GetBoundingRectangles() | Where-Object { $_.Width -gt 0 -and $_.Height -gt 0 })
        }
        if ($rectangles.Count -gt 0) {
            $left = ($rectangles | Measure-Object Left -Minimum).Minimum
            $top = ($rectangles | Measure-Object Top -Minimum).Minimum
            $right = ($rectangles | Measure-Object Right -Maximum).Maximum
            $bottom = ($rectangles | Measure-Object Bottom -Maximum).Maximum
            @{Left=[int][math]::Floor($left);Top=[int][math]::Floor($top);Right=[int][math]::Ceiling($right);Bottom=[int][math]::Ceiling($bottom)} | ConvertTo-Json -Compress
            exit
        }
    }
    $element = $walker.GetParent($element)
}
`
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	var bounds model.SelectionBounds
	if json.Unmarshal(output, &bounds) != nil || bounds.Right <= bounds.Left || bounds.Bottom <= bounds.Top {
		return nil
	}
	return &bounds
}
