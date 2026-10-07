//go:build windows

package settings

import (
	"fmt"
	"github.com/lxn/walk"
	"strings"
	"touchdict/internal/localmodel"
	"touchdict/internal/uistyle"
)

type modelTabs struct {
	tabs           *walk.TabWidget
	key, directory *walk.LineEdit
	online, local  *walk.ComboBox
	onlineModels   []string
	localModels    []localmodel.Model
	scanned        string
}

func newModelTabs(d *walk.Dialog, cfg *Config) *modelTabs {
	m := &modelTabs{}
	m.tabs, _ = walk.NewTabWidget(d)
	localPage, _ := walk.NewTabPage()
	localPage.SetTitle("本地")
	localPage.SetLayout(walk.NewVBoxLayout())
	m.tabs.Pages().Add(localPage)
	label, _ := walk.NewTextLabel(localPage)
	label.SetText("模型目录（检索 GGUF 文件）")
	row, _ := walk.NewComposite(localPage)
	row.SetLayout(walk.NewHBoxLayout())
	m.directory, _ = walk.NewLineEdit(row)
	m.directory.SetText(cfg.LocalModelDir)
	scan, _ := walk.NewPushButton(row)
	scan.SetText("检索")
	uistyle.FitButton(scan)
	m.local, _ = walk.NewDropDownBox(localPage)
	status, _ := walk.NewTextLabel(localPage)
	search := func() {
		m.localModels = nil
		m.scanned = ""
		m.local.SetModel([]string{})
		models, err := localmodel.Discover(m.directory.Text())
		if err != nil {
			status.SetText(err.Error())
			return
		}
		m.localModels, m.scanned = models, strings.TrimSpace(m.directory.Text())
		names := make([]string, len(models))
		selected := 0
		for i, model := range models {
			names[i] = model.Name
			if strings.EqualFold(model.Path, cfg.LocalModel) {
				selected = i
			}
		}
		m.local.SetModel(names)
		if len(models) > 0 {
			m.local.SetCurrentIndex(selected)
		}
		status.SetText(fmt.Sprintf("已找到 %d 个模型；保存后使用当前标签中的模型。", len(models)))
	}
	scan.Clicked().Attach(search)
	m.directory.TextChanged().Attach(func() {
		m.localModels = nil
		m.scanned = ""
		m.local.SetModel([]string{})
		status.SetText("路径已修改，请点击检索。")
	})
	search()
	onlinePage, _ := walk.NewTabPage()
	onlinePage.SetTitle("在线")
	onlinePage.SetLayout(walk.NewVBoxLayout())
	m.tabs.Pages().Add(onlinePage)
	link, _ := walk.NewLinkLabel(onlinePage)
	link.SetText(`Gemini API Key  <a href="https://aistudio.google.com/apikey">申请</a>`)
	link.LinkActivated().Attach(func(link *walk.LinkLabelLink) {
		if !openURL(uintptr(d.Handle()), link.URL()) {
			walk.MsgBox(d, "无法打开网页", "请在浏览器中打开："+geminiKeyURL, walk.MsgBoxIconWarning)
		}
	})
	m.key, _ = walk.NewLineEdit(onlinePage)
	m.key.SetPasswordMode(true)
	m.key.SetText(cfg.APIKey)
	onlineLabel, _ := walk.NewTextLabel(onlinePage)
	onlineLabel.SetText("Gemini 模型（支持免费层级）")
	m.onlineModels = append([]string(nil), GeminiModels...)
	selected, found := 0, false
	for i, name := range m.onlineModels {
		if name == cfg.Model {
			selected, found = i, true
		}
	}
	if !found && cfg.Model != "" {
		m.onlineModels = append(m.onlineModels, cfg.Model)
		selected = len(m.onlineModels) - 1
	}
	m.online, _ = walk.NewDropDownBox(onlinePage)
	m.online.SetModel(m.onlineModels)
	m.online.SetCurrentIndex(selected)
	if cfg.ModelProvider == "local" {
		m.tabs.SetCurrentIndex(0)
	} else {
		m.tabs.SetCurrentIndex(1)
	}
	return m
}

func (m *modelTabs) save(cfg *Config) error {
	cfg.APIKey = m.key.Text()
	if i := m.online.CurrentIndex(); i >= 0 && i < len(m.onlineModels) {
		cfg.Model = m.onlineModels[i]
	}
	cfg.LocalModelDir = strings.TrimSpace(m.directory.Text())
	cfg.LocalModel = ""
	if i := m.local.CurrentIndex(); i >= 0 && i < len(m.localModels) && strings.EqualFold(m.scanned, cfg.LocalModelDir) {
		cfg.LocalModel = m.localModels[i].Path
	}
	cfg.ModelProvider = "online"
	if m.tabs.CurrentIndex() == 0 {
		if cfg.LocalModel == "" {
			return fmt.Errorf("请先检索模型目录并选择一个本地模型")
		}
		cfg.ModelProvider = "local"
	}
	return nil
}
