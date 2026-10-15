//go:build windows

package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"touchdict/internal/online"
)

type modelSpeed struct {
	Milliseconds int64     `json:"milliseconds"`
	Error        string    `json:"error,omitempty"`
	TestedAt     time.Time `json:"testedAt"`
}

var speeds = struct {
	sync.Mutex
	once      sync.Once
	results   map[string]modelSpeed
	running   bool
	nextID    int
	observers map[int]func()
}{results: make(map[string]modelSpeed), observers: make(map[int]func())}

func speedPath() string {
	base, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "TouchDict", "model-speeds.json")
}

func loadSpeeds() {
	speeds.once.Do(func() {
		data, err := os.ReadFile(speedPath())
		if err == nil {
			var saved map[string]modelSpeed
			if json.Unmarshal(data, &saved) == nil && saved != nil {
				speeds.results = saved
			}
		}
	})
}

func OnlineModelLabel(name string) string {
	speeds.Lock()
	defer speeds.Unlock()
	loadSpeeds()
	result, ok := speeds.results[name]
	label := online.Label(name)
	if !ok {
		return label
	}
	if result.Error != "" {
		status := "失败"
		switch {
		case strings.Contains(result.Error, "404"):
			status = "不可用（404）"
		case strings.Contains(result.Error, "503"):
			status = "服务繁忙（503）"
		case strings.Contains(result.Error, "429"):
			status = "额度受限（429）"
		case strings.Contains(result.Error, "超时"):
			status = "超时"
		}
		return label + " · " + status
	}
	return fmt.Sprintf("%s · %d ms", label, result.Milliseconds)
}

func OnlineModelLabels(names []string) []string {
	labels := make([]string, len(names))
	for i, name := range names {
		labels[i] = OnlineModelLabel(name)
	}
	return labels
}

func SubscribeModelSpeeds(callback func()) func() {
	speeds.Lock()
	speeds.nextID++
	id := speeds.nextID
	speeds.observers[id] = callback
	speeds.Unlock()
	return func() { speeds.Lock(); delete(speeds.observers, id); speeds.Unlock() }
}

// Run sequentially for comparable measurements and to avoid request bursts.
// Each completed result is persisted and both model lists are notified.
func MeasureAllOnlineModels(ctx context.Context, keys map[string]string, names []string, progress func(string)) error {
	var configured []string
	for _, name := range names {
		if strings.TrimSpace(keys[name]) != "" {
			configured = append(configured, name)
		}
	}
	if len(configured) == 0 {
		return fmt.Errorf("请先填写至少一个在线平台的 API Key")
	}
	skipped := len(names) - len(configured)
	names = configured
	speeds.Lock()
	loadSpeeds()
	if speeds.running {
		speeds.Unlock()
		return fmt.Errorf("在线模型正在测速，请等待完成")
	}
	speeds.running = true
	speeds.Unlock()
	defer func() { speeds.Lock(); speeds.running = false; speeds.Unlock() }()
	for i, name := range names {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		progress(fmt.Sprintf("测速中 %d/%d：%s", i+1, len(names), online.Label(name)))
		elapsed, err := online.MeasureQuery(ctx, keys[name], name)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result := modelSpeed{Milliseconds: elapsed.Milliseconds(), TestedAt: time.Now()}
		if err != nil {
			result.Error = err.Error()
		}
		speeds.Lock()
		speeds.results[name] = result
		data, saveErr := json.MarshalIndent(speeds.results, "", "  ")
		path := speedPath()
		if saveErr == nil && path != "" {
			saveErr = os.MkdirAll(filepath.Dir(path), 0700)
			if saveErr == nil {
				saveErr = os.WriteFile(path, data, 0600)
			}
		}
		callbacks := make([]func(), 0, len(speeds.observers))
		for _, cb := range speeds.observers {
			callbacks = append(callbacks, cb)
		}
		speeds.Unlock()
		for _, cb := range callbacks {
			cb()
		}
		if saveErr != nil {
			return fmt.Errorf("测速结果保存失败：%w", saveErr)
		}
	}
	progress(fmt.Sprintf("测速完成：%d 个模型，跳过 %d 个未配置 Key 的模型", len(names), skipped))
	return nil
}
