//go:build windows

package settings

import (
	"context"
	"fmt"
	"github.com/lxn/walk"
	"strings"
	"touchdict/internal/chatgpt"
	"touchdict/internal/localmodel"
	"touchdict/internal/uistyle"
)

type modelTabs struct {
	tabs                   *walk.TabWidget
	key, directory         *walk.LineEdit
	groqKey, openRouterKey *walk.LineEdit
	online, local          *walk.ComboBox
	onlineModels           []string
	subscription           *walk.ComboBox
	subscriptionModels     []string
	localModels            []localmodel.Model
	scanned                string
}

func newModelTabs(d *walk.Dialog, cfg *Config) *modelTabs {
	currentOnlineModel := NormalizeGeminiModel(cfg.Model)
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
	newProviderKey := func(name, url, value string) *walk.LineEdit {
		link, _ := walk.NewLinkLabel(onlinePage)
		link.SetText(name + ` API Key  <a href="` + url + `">申请免费 Key</a>`)
		link.LinkActivated().Attach(func(link *walk.LinkLabelLink) {
			if !openURL(uintptr(d.Handle()), link.URL()) {
				walk.MsgBox(d, "无法打开网页", "请在浏览器中打开："+link.URL(), walk.MsgBoxIconWarning)
			}
		})
		key, _ := walk.NewLineEdit(onlinePage)
		key.SetPasswordMode(true)
		key.SetText(value)
		return key
	}
	m.groqKey = newProviderKey("Groq", "https://console.groq.com/keys", cfg.GroqAPIKey)
	m.openRouterKey = newProviderKey("OpenRouter", "https://openrouter.ai/settings/keys", cfg.OpenRouterAPIKey)
	freeInfo, _ := walk.NewTextLabel(onlinePage)
	freeInfo.SetText("免费额度有频率限制；OpenRouter 仅使用免费路由。Key 留空的平台跳过测速。")
	onlineLabel, _ := walk.NewTextLabel(onlinePage)
	onlineLabel.SetText("在线模型（支持免费额度）")
	m.onlineModels = OnlineModels()
	selected, found := 0, false
	for i, name := range m.onlineModels {
		if name == currentOnlineModel {
			selected, found = i, true
		}
	}
	if !found && currentOnlineModel != "" {
		m.onlineModels = append(m.onlineModels, currentOnlineModel)
		selected = len(m.onlineModels) - 1
	}
	m.online, _ = walk.NewDropDownBox(onlinePage)
	m.online.SetModel(OnlineModelLabels(m.onlineModels))
	m.online.SetCurrentIndex(selected)
	speedButton, _ := walk.NewPushButton(onlinePage)
	speedButton.SetText("测试全部在线模型查询速度")
	uistyle.FitButton(speedButton)
	speedResult, _ := walk.NewTextLabel(onlinePage)
	speedResult.SetText("最近一次测速结果显示在模型名后（ms）")
	speedCtx, cancelSpeed := context.WithCancel(context.Background())
	unsubscribe := SubscribeModelSpeeds(func() {
		if speedCtx.Err() != nil {
			return
		}
		d.Synchronize(func() {
			if speedCtx.Err() != nil {
				return
			}
			i := m.online.CurrentIndex()
			m.online.SetModel(OnlineModelLabels(m.onlineModels))
			m.online.SetCurrentIndex(i)
		})
	})
	d.Disposing().Attach(func() { cancelSpeed(); unsubscribe() })
	speedButton.Clicked().Attach(func() {
		names := append([]string(nil), m.onlineModels...)
		draft := *cfg
		draft.APIKey, draft.GroqAPIKey, draft.OpenRouterAPIKey = m.key.Text(), m.groqKey.Text(), m.openRouterKey.Text()
		keys := draft.ModelKeys(names)
		speedButton.SetEnabled(false)
		speedResult.SetText("正在测试全部在线模型…")
		go func() {
			err := MeasureAllOnlineModels(speedCtx, keys, names, func(status string) {
				if speedCtx.Err() != nil {
					return
				}
				d.Synchronize(func() {
					if speedCtx.Err() == nil {
						speedResult.SetText(status)
					}
				})
			})
			if speedCtx.Err() != nil {
				return
			}
			d.Synchronize(func() {
				if speedCtx.Err() != nil {
					return
				}
				speedButton.SetEnabled(true)
				if err != nil {
					speedResult.SetText(err.Error())
				}
			})
		}()
	})
	if cfg.ModelProvider == "local" {
		m.tabs.SetCurrentIndex(0)
	} else {
		m.tabs.SetCurrentIndex(1)
	}
	m.addSubscription(d, cfg)
	return m
}

func (m *modelTabs) save(cfg *Config) error {
	if m.tabs.CurrentIndex() == 2 {
		if !chatgpt.Default.Connected() {
			return fmt.Errorf("请先点击授权连接 ChatGPT 订阅")
		}
		i := m.subscription.CurrentIndex()
		if i < 0 || i >= len(m.subscriptionModels) {
			return fmt.Errorf("请先刷新并选择订阅模型")
		}
		cfg.ModelProvider, cfg.Model = "online", m.subscriptionModels[i]
		return nil
	}
	cfg.APIKey = m.key.Text()
	cfg.GroqAPIKey, cfg.OpenRouterAPIKey = strings.TrimSpace(m.groqKey.Text()), strings.TrimSpace(m.openRouterKey.Text())
	if i := m.online.CurrentIndex(); i >= 0 && i < len(m.onlineModels) {
		cfg.Model = NormalizeGeminiModel(m.onlineModels[i])
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
