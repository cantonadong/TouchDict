//go:build windows

package main

import (
	"github.com/lxn/walk"
	"path/filepath"
	"touchdict/internal/localmodel"
	"touchdict/internal/settings"
)

func addModelMenu(notify *walk.NotifyIcon, cfg *settings.Config, owner walk.Form, changed func()) (func(), error) {
	current := walk.NewAction()
	current.SetEnabled(false)
	if err := notify.ContextMenu().Actions().Add(current); err != nil {
		return nil, err
	}
	online, err := walk.NewMenu()
	if err != nil {
		return nil, err
	}
	local, err := walk.NewMenu()
	if err != nil {
		online.Dispose()
		return nil, err
	}
	for _, item := range []struct {
		name string
		menu *walk.Menu
	}{{"在线模型", online}, {"本地模型", local}} {
		parent := walk.NewMenuAction(item.menu)
		parent.SetText(item.name)
		if err := notify.ContextMenu().Actions().Add(parent); err != nil {
			return nil, err
		}
	}
	var refresh func()
	selectModel := func(provider, name string) {
		updated := *cfg
		updated.ModelProvider = provider
		if provider == "local" {
			updated.LocalModel = name
		} else {
			updated.Model = name
		}
		if err := updated.Save(); err != nil {
			walk.MsgBox(owner, "TouchDict", "模型切换保存失败："+err.Error(), walk.MsgBoxIconError)
			refresh()
			return
		}
		*cfg = updated
		changed()
		refresh()
	}
	refresh = func() {
		online.Actions().Clear()
		local.Actions().Clear()
		label := "在线 " + cfg.Model
		if cfg.ModelProvider == "local" {
			label = "本地 " + filepath.Base(cfg.LocalModel)
		}
		current.SetText("当前模型：" + label)
		names := append([]string(nil), settings.GeminiModels...)
		found := false
		for _, name := range names {
			if name == cfg.Model {
				found = true
			}
		}
		if !found && cfg.Model != "" {
			names = append(names, cfg.Model)
		}
		for _, name := range names {
			name := name
			action := walk.NewAction()
			action.SetText(name)
			action.SetCheckable(true)
			action.SetChecked(cfg.ModelProvider != "local" && cfg.Model == name)
			action.Triggered().Attach(func() { selectModel("online", name) })
			online.Actions().Add(action)
		}
		models, err := localmodel.Discover(cfg.LocalModelDir)
		if err != nil || len(models) == 0 {
			action := walk.NewAction()
			action.SetText("未找到模型，请在设置中检索")
			action.SetEnabled(false)
			local.Actions().Add(action)
		}
		for _, model := range models {
			model := model
			action := walk.NewAction()
			action.SetText(model.Name)
			action.SetCheckable(true)
			action.SetChecked(cfg.ModelProvider == "local" && cfg.LocalModel == model.Path)
			action.Triggered().Attach(func() { selectModel("local", model.Path) })
			local.Actions().Add(action)
		}
	}
	refresh()
	return refresh, nil
}
