//go:build windows

package settings

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/lxn/walk"
	"os"
	"path/filepath"
	"strings"
	"time"
	"touchdict/internal/chatgpt"
	"touchdict/internal/uistyle"
)

func configureSubscription(dir string) error {
	path := filepath.Join(dir, "chatgpt-session.dat")
	var saved chatgpt.Saved
	raw, err := os.ReadFile(path)
	if err == nil {
		plain, e := unprotect(raw)
		if e != nil {
			return errors.New("订阅凭据无法解密，请重新授权")
		}
		if json.Unmarshal([]byte(plain), &saved) != nil {
			return errors.New("订阅凭据损坏，请重新授权")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	chatgpt.Default.Configure(saved, subscriptionPersistence(dir))
	return nil
}

func subscriptionPersistence(dir string) func(chatgpt.Saved) error {
	return func(value chatgpt.Saved) error {
		b, e := json.Marshal(value)
		if e != nil {
			return e
		}
		encrypted, e := protect(string(b))
		if e != nil {
			return e
		}
		if e = os.MkdirAll(dir, 0700); e != nil {
			return e
		}
		f, e := os.CreateTemp(dir, "chatgpt-session-*.tmp")
		if e != nil {
			return e
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		if _, e = f.Write(encrypted); e != nil {
			f.Close()
			return e
		}
		if e = f.Sync(); e != nil {
			f.Close()
			return e
		}
		if e = f.Close(); e != nil {
			return e
		}
		return os.Rename(tmp, filepath.Join(dir, "chatgpt-session.dat"))
	}
}

func (m *modelTabs) addSubscription(d *walk.Dialog, cfg *Config) {
	page, _ := walk.NewTabPage()
	page.SetTitle("订阅")
	page.SetLayout(walk.NewVBoxLayout())
	m.tabs.Pages().Add(page)
	info, _ := walk.NewTextLabel(page)
	info.SetText("使用个人 ChatGPT / Codex 订阅查词。点击授权后在浏览器确认账号，完成后自动连接。")
	status, _ := walk.NewTextLabel(page)
	status.SetText(chatgpt.Default.Status())
	connect, _ := walk.NewPushButton(page)
	connect.SetText("Continue with ChatGPT · 点击授权并启用")
	uistyle.FitButton(connect)
	disconnect, _ := walk.NewPushButton(page)
	disconnect.SetText("断开连接")
	uistyle.FitButton(disconnect)
	label, _ := walk.NewTextLabel(page)
	label.SetText("订阅模型")
	m.subscription, _ = walk.NewDropDownBox(page)
	updateModels := func() {
		models := chatgpt.Default.Models()
		m.subscriptionModels = nil
		labels := []string{}
		selected := 0
		for i, v := range models {
			m.subscriptionModels = append(m.subscriptionModels, "chatgpt:"+v.Slug)
			labels = append(labels, v.Name)
			if cfg.Model == "chatgpt:"+v.Slug {
				selected = i
			}
		}
		m.subscription.SetModel(labels)
		if len(labels) > 0 {
			m.subscription.SetCurrentIndex(selected)
		}
	}
	updateModels()
	refresh, _ := walk.NewPushButton(page)
	refresh.SetText("刷新模型列表")
	uistyle.FitButton(refresh)
	note, _ := walk.NewTextLabel(page)
	note.SetText("连接会加密保存并自动续期。授权成功后自动启用订阅查词。使用受账号订阅额度限制。")
	lifetime, cancelLifetime := context.WithCancel(context.Background())
	var cancelAttempt context.CancelFunc
	cancelLogin, _ := walk.NewPushButton(page)
	cancelLogin.SetText("取消本次授权")
	uistyle.FitButton(cancelLogin)
	cancelLogin.SetEnabled(false)
	cancelLogin.Clicked().Attach(func() {
		if cancelAttempt != nil {
			cancelAttempt()
		}
	})
	d.Disposing().Attach(func() {
		cancelLifetime()
		if cancelAttempt != nil {
			cancelAttempt()
		}
	})
	busy := func(value bool) {
		connect.SetEnabled(!value)
		disconnect.SetEnabled(!value)
		refresh.SetEnabled(!value)
		cancelLogin.SetEnabled(value && cancelAttempt != nil)
	}
	loadModels := func(ctx context.Context, activate bool) {
		_, err := chatgpt.Default.RefreshModels(ctx)
		if lifetime.Err() != nil {
			return
		}
		d.Synchronize(func() {
			if lifetime.Err() != nil {
				return
			}
			busy(false)
			updateModels()
			status.SetText(chatgpt.Default.Status())
			if err != nil {
				status.SetText(chatgpt.Default.Status() + "；模型列表：" + err.Error())
			} else if activate && len(m.subscriptionModels) > 0 {
				updated := *cfg
				updated.ModelProvider = "online"
				updated.Model = m.subscriptionModels[m.subscription.CurrentIndex()]
				if saveErr := updated.Save(); saveErr != nil {
					status.SetText("已连接；启用失败：" + saveErr.Error())
				} else {
					*cfg = updated
					status.SetText(chatgpt.Default.Status() + "；已启用，可以关闭设置开始查词")
				}
			}
		})
	}
	refresh.Clicked().Attach(func() { busy(true); status.SetText("正在获取订阅模型…"); go loadModels(lifetime, false) })
	connect.Clicked().Attach(func() {
		if cancelAttempt != nil {
			cancelAttempt()
		}
		ctx, cancel := context.WithTimeout(lifetime, 5*time.Minute)
		cancelAttempt = cancel
		a, err := chatgpt.Default.Begin(ctx)
		if err != nil {
			cancel()
			status.SetText(err.Error())
			return
		}
		if !openURL(uintptr(d.Handle()), a.URL) {
			a.Close()
			cancel()
			status.SetText("无法打开浏览器，请检查默认浏览器后重试")
			return
		}
		busy(true)
		status.SetText("等待浏览器授权…")
		go func() {
			defer cancel()
			err := chatgpt.Default.Finish(ctx, a)
			if lifetime.Err() != nil {
				return
			}
			if err != nil {
				d.Synchronize(func() {
					if lifetime.Err() == nil {
						busy(false)
						status.SetText(err.Error())
					}
				})
				return
			}
			d.Synchronize(func() {
				if lifetime.Err() == nil {
					status.SetText(chatgpt.Default.Status() + "；正在获取模型…")
					m.tabs.SetCurrentIndex(2)
				}
			})
			loadModels(ctx, true)
		}()
	})
	disconnect.Clicked().Attach(func() {
		busy(true)
		status.SetText("正在断开连接…")
		go func() {
			err := chatgpt.Default.Disconnect(lifetime)
			if lifetime.Err() != nil {
				return
			}
			d.Synchronize(func() {
				if lifetime.Err() != nil {
					return
				}
				busy(false)
				updateModels()
				status.SetText(chatgpt.Default.Status())
				if err != nil {
					status.SetText(err.Error())
				}
			})
		}()
	})
	if strings.HasPrefix(cfg.Model, "chatgpt:") {
		m.tabs.SetCurrentIndex(2)
	}
}
