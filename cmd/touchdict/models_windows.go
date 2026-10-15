//go:build windows

package main

import (
	"context"
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
	speed := walk.NewAction()
	speed.SetText("测试全部在线模型查询速度")
	if err := notify.ContextMenu().Actions().Add(speed); err != nil {
		return nil, err
	}
	speedResult := walk.NewAction()
	speedResult.SetEnabled(false)
	speedResult.SetText("查询耗时：尚未测试")
	if err := notify.ContextMenu().Actions().Add(speedResult); err != nil {
		return nil, err
	}
	speedCtx, cancelSpeed := context.WithCancel(context.Background())
	owner.Disposing().Attach(func() { cancelSpeed() })
	var refresh func()
	speed.Triggered().Attach(func() {
		names := settings.OnlineModels()
		name := settings.NormalizeGeminiModel(cfg.Model)
		found := false
		for _, candidate := range names {
			if candidate == name {
				found = true
			}
		}
		if !found && name != "" {
			names = append(names, name)
		}
		keys := cfg.ModelKeys(names)
		speed.SetEnabled(false)
		speedResult.SetText("正在测试全部在线模型…")
		go func() {
			err := settings.MeasureAllOnlineModels(speedCtx, keys, names, func(status string) {
				if speedCtx.Err() != nil {
					return
				}
				owner.Synchronize(func() {
					if speedCtx.Err() == nil {
						speedResult.SetText(status)
					}
				})
			})
			if speedCtx.Err() != nil {
				return
			}
			owner.Synchronize(func() {
				if speedCtx.Err() != nil {
					return
				}
				speed.SetEnabled(true)
				if err != nil {
					speedResult.SetText(err.Error())
				}
			})
		}()
	})
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
		cfg.Model = settings.NormalizeGeminiModel(cfg.Model)
		online.Actions().Clear()
		local.Actions().Clear()
		label := "在线 " + settings.OnlineModelLabel(cfg.Model)
		if cfg.ModelProvider == "local" {
			label = "本地 " + filepath.Base(cfg.LocalModel)
		}
		current.SetText("当前模型：" + label)
		names := settings.OnlineModels()
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
			action.SetText(settings.OnlineModelLabel(name))
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
	unsubscribe := settings.SubscribeModelSpeeds(func() {
		if speedCtx.Err() != nil {
			return
		}
		owner.Synchronize(func() {
			if speedCtx.Err() == nil {
				refresh()
			}
		})
	})
	owner.Disposing().Attach(func() { unsubscribe() })
	return refresh, nil
}
