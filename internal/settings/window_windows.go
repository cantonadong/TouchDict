//go:build windows

package settings

import "github.com/lxn/walk"

var GeminiModels = []string{
	"gemini-flash-lite-latest",
	"gemini-flash-latest",
}

func Edit(owner walk.Form, cfg *Config) bool {
	d, err := walk.NewDialog(owner)
	if err != nil {
		return false
	}
	defer d.Dispose()
	d.SetTitle("TouchDict 设置")
	if icon, iconErr := walk.NewIconFromResourceId(2); iconErr == nil {
		_ = d.SetIcon(icon)
		defer icon.Dispose()
	}
	d.SetSize(walk.Size{Width: 500, Height: 280})
	l := walk.NewVBoxLayout()
	l.SetMargins(walk.Margins{HNear: 18, VNear: 18, HFar: 18, VFar: 18})
	l.SetSpacing(6)
	_ = d.SetLayout(l)
	label, _ := walk.NewTextLabel(d)
	label.SetText("Gemini API Key")
	_ = label.SetTextAlignment(walk.AlignHNearVNear)
	key, _ := walk.NewLineEdit(d)
	key.SetPasswordMode(true)
	key.SetText(cfg.APIKey)
	gap, _ := walk.NewVSpacer(d)
	_ = gap.SetMinMaxSize(walk.Size{Height: 8}, walk.Size{Height: 8})
	modelLabel, _ := walk.NewTextLabel(d)
	modelLabel.SetText("Gemini 模型")
	_ = modelLabel.SetTextAlignment(walk.AlignHNearVNear)
	models := append([]string(nil), GeminiModels...)
	modelIndex := 0
	found := false
	for i, name := range models {
		if name == cfg.Model {
			modelIndex, found = i, true
			break
		}
	}
	if !found && cfg.Model != "" {
		models = append(models, cfg.Model)
		modelIndex = len(models) - 1
	}
	modelDrop, _ := walk.NewDropDownBox(d)
	_ = modelDrop.SetModel(models)
	_ = modelDrop.SetCurrentIndex(modelIndex)
	newLeftCheck := func(text string, checked bool) *walk.CheckBox {
		checkRow, _ := walk.NewComposite(d)
		checkLayout := walk.NewHBoxLayout()
		checkLayout.SetMargins(walk.Margins{})
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
	row, _ := walk.NewComposite(d)
	_ = row.SetLayout(walk.NewHBoxLayout())
	_, _ = walk.NewHSpacer(row)
	cancel, _ := walk.NewPushButton(row)
	cancel.SetText("取消")
	cancel.Clicked().Attach(func() { d.Cancel() })
	save, _ := walk.NewPushButton(row)
	save.SetText("保存")
	save.Clicked().Attach(func() {
		cfg.APIKey = key.Text()
		if i := modelDrop.CurrentIndex(); i >= 0 && i < len(models) {
			cfg.Model = models[i]
		}
		cfg.AltEnabled = alt.Checked()
		cfg.HotkeyEnabled = hot.Checked()
		cfg.AutoSpeak = auto.Checked()
		if err := cfg.Save(); err != nil {
			walk.MsgBox(d, "保存失败", err.Error(), walk.MsgBoxIconError)
			return
		}
		d.Accept()
	})
	return d.Run() == walk.DlgCmdOK
}
