//go:build windows

package settings

import (
	"github.com/lxn/walk"
	"syscall"
	"touchdict/internal/uistyle"
	"unsafe"
)

const geminiKeyURL = "https://aistudio.google.com/apikey"

var shellExecute = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

func Edit(owner walk.Form, cfg *Config, exePath string) bool {
	d, err := walk.NewDialog(owner)
	if err != nil {
		return false
	}
	defer d.Dispose()
	if font, fontErr := walk.NewFont("Microsoft YaHei UI", 9, 0); fontErr == nil {
		d.SetFont(font)
	}
	d.SetTitle("TouchDict 设置")
	if icon, iconErr := walk.NewIconFromResourceId(2); iconErr == nil {
		_ = d.SetIcon(icon)
		defer icon.Dispose()
	}
	d.SetSize(walk.Size{Width: 620, Height: 500})
	l := walk.NewVBoxLayout()
	l.SetMargins(walk.Margins{HNear: 36, VNear: 36, HFar: 36, VFar: 36})
	l.SetSpacing(12)
	_ = d.SetLayout(l)
	modelSelection := newModelTabs(d, cfg)
	newLeftCheck := func(text string, checked bool) *walk.CheckBox {
		checkRow, _ := walk.NewComposite(d)
		checkLayout := walk.NewHBoxLayout()
		checkLayout.SetMargins(walk.Margins{})
		checkLayout.SetSpacing(12)
		_ = checkRow.SetLayout(checkLayout)
		box, _ := walk.NewCheckBox(checkRow)
		box.SetText(text)
		box.SetChecked(checked)
		_, _ = walk.NewHSpacer(checkRow)
		return box
	}
	alt := newLeftCheck("启用三指点按（需开启触控板左 Alt 触发）", cfg.AltEnabled)
	hot := newLeftCheck("启用 Ctrl+Alt+D", cfg.HotkeyEnabled)
	auto := newLeftCheck("查询成功后自动发音", cfg.AutoSpeak)
	startup := newLeftCheck("开机自动启动", cfg.StartupEnabled)
	row, _ := walk.NewComposite(d)
	buttonLayout := walk.NewHBoxLayout()
	buttonLayout.SetSpacing(12)
	_ = row.SetLayout(buttonLayout)
	_, _ = walk.NewHSpacer(row)
	cancel, _ := walk.NewPushButton(row)
	cancel.SetText("取消")
	cancel.Clicked().Attach(func() { d.Cancel() })
	save, _ := walk.NewPushButton(row)
	save.SetText("保存")
	save.Clicked().Attach(func() {
		updated := *cfg
		if err := modelSelection.save(&updated); err != nil {
			walk.MsgBox(d, "无法保存", err.Error(), walk.MsgBoxIconWarning)
			return
		}
		updated.AltEnabled = alt.Checked()
		updated.HotkeyEnabled = hot.Checked()
		updated.AutoSpeak = auto.Checked()
		updated.StartupEnabled = startup.Checked()
		if err := SyncStartup(exePath, updated.StartupEnabled); err != nil {
			walk.MsgBox(d, "保存失败", err.Error(), walk.MsgBoxIconError)
			return
		}
		if err := updated.Save(); err != nil {
			_ = SyncStartup(exePath, cfg.StartupEnabled)
			walk.MsgBox(d, "保存失败", err.Error(), walk.MsgBoxIconError)
			return
		}
		*cfg = updated
		d.Accept()
	})
	uistyle.FitButton(cancel)
	uistyle.FitButton(save)
	return d.Run() == walk.DlgCmdOK
}

func openURL(owner uintptr, target string) bool {
	verb, _ := syscall.UTF16PtrFromString("open")
	url, _ := syscall.UTF16PtrFromString(target)
	r, _, _ := shellExecute.Call(owner, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(url)), 0, 0, 1)
	return r > 32
}
