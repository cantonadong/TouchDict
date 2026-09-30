//go:build windows

package gemini

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"touchdict/internal/model"
	"unsafe"
)

const cacheLimit = 500

var cacheFile string

type diskCache struct {
	Entries []diskCacheEntry `json:"entries"`
}

type diskCacheEntry struct {
	Key        string           `json:"key"`
	Query      string           `json:"query,omitempty"`
	Context    string           `json:"context,omitempty"`
	Definition model.Definition `json:"definition"`
}

var historySubscribers = map[int]func([]model.HistoryEntry){}
var nextSubscriberID int
var historyMeta = map[string]model.HistoryEntry{}

var (
	cacheKernel32 = syscall.NewLazyDLL("kernel32.dll")
	moveFileEx    = cacheKernel32.NewProc("MoveFileExW")
)

// ConfigureCache loads the persistent cache stored beside the executable.
func ConfigureCache(exeDir string) error {
	path := filepath.Join(exeDir, "touchdict_cache.json")
	legacyPath := filepath.Join(exeDir, ".touchdict-cache", "cache.json")

	resultCache.Lock()
	defer resultCache.Unlock()
	cacheFile = path
	resultCache.items = make(map[string]model.Definition)
	resultCache.order = nil
	historyMeta = make(map[string]model.HistoryEntry)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		b, err = os.ReadFile(legacyPath)
		if os.IsNotExist(err) {
			return nil
		}
	}
	if err != nil {
		return fmt.Errorf("read cache: %w", err)
	}
	var saved diskCache
	if err := json.Unmarshal(b, &saved); err != nil {
		return fmt.Errorf("parse cache: %w", err)
	}
	start := 0
	if len(saved.Entries) > cacheLimit {
		start = len(saved.Entries) - cacheLimit
	}
	for _, entry := range saved.Entries[start:] {
		if entry.Key == "" || entry.Definition.Term == "" {
			continue
		}
		removeOrderKeyLocked(entry.Key)
		resultCache.items[entry.Key] = entry.Definition
		query := entry.Query
		if query == "" {
			query = entry.Definition.Term
		}
		historyMeta[entry.Key] = model.HistoryEntry{Key: entry.Key, Query: query, Context: entry.Context, Definition: entry.Definition}
		resultCache.order = append(resultCache.order, entry.Key)
	}
	return saveCacheLocked()
}

func storeCached(key string, selection model.Selection, definition model.Definition) {
	resultCache.Lock()
	removeOrderKeyLocked(key)
	resultCache.items[key] = definition
	historyMeta[key] = model.HistoryEntry{Key: key, Query: selection.Text, Context: selection.Context, Definition: definition}
	resultCache.order = append(resultCache.order, key)
	for len(resultCache.order) > cacheLimit {
		delete(resultCache.items, resultCache.order[0])
		delete(historyMeta, resultCache.order[0])
		resultCache.order = resultCache.order[1:]
	}
	_ = saveCacheLocked()
	snapshot, callbacks := historyNotificationLocked()
	resultCache.Unlock()
	notifyHistory(snapshot, callbacks)
}

func History() []model.HistoryEntry {
	resultCache.Lock()
	defer resultCache.Unlock()
	return historyLocked()
}

func Touch(key string) {
	resultCache.Lock()
	if _, ok := resultCache.items[key]; !ok {
		resultCache.Unlock()
		return
	}
	removeOrderKeyLocked(key)
	resultCache.order = append(resultCache.order, key)
	_ = saveCacheLocked()
	snapshot, callbacks := historyNotificationLocked()
	resultCache.Unlock()
	notifyHistory(snapshot, callbacks)
}

func HistoryDefinition(key string) (model.Definition, bool) {
	resultCache.Lock()
	defer resultCache.Unlock()
	d, ok := resultCache.items[key]
	return d, ok
}

func SubscribeHistory(fn func([]model.HistoryEntry)) func() {
	resultCache.Lock()
	id := nextSubscriberID
	nextSubscriberID++
	historySubscribers[id] = fn
	resultCache.Unlock()
	return func() { resultCache.Lock(); delete(historySubscribers, id); resultCache.Unlock() }
}

func historyLocked() []model.HistoryEntry {
	result := make([]model.HistoryEntry, 0, len(resultCache.order))
	for i := len(resultCache.order) - 1; i >= 0; i-- {
		result = append(result, historyMeta[resultCache.order[i]])
	}
	return result
}

func historyNotificationLocked() ([]model.HistoryEntry, []func([]model.HistoryEntry)) {
	snapshot := historyLocked()
	callbacks := make([]func([]model.HistoryEntry), 0, len(historySubscribers))
	for _, fn := range historySubscribers {
		callbacks = append(callbacks, fn)
	}
	return snapshot, callbacks
}

func notifyHistory(snapshot []model.HistoryEntry, callbacks []func([]model.HistoryEntry)) {
	for _, fn := range callbacks {
		fn(append([]model.HistoryEntry(nil), snapshot...))
	}
}

func removeOrderKeyLocked(key string) {
	for i, existing := range resultCache.order {
		if existing == key {
			resultCache.order = append(resultCache.order[:i], resultCache.order[i+1:]...)
			return
		}
	}
}

func saveCacheLocked() error {
	if cacheFile == "" {
		return nil
	}
	saved := diskCache{Entries: make([]diskCacheEntry, 0, len(resultCache.order))}
	for _, key := range resultCache.order {
		meta := historyMeta[key]
		saved.Entries = append(saved.Entries, diskCacheEntry{Key: key, Query: meta.Query, Context: meta.Context, Definition: resultCache.items[key]})
	}
	b, err := json.MarshalIndent(saved, "", "  ")
	if err != nil {
		return err
	}
	tmp := cacheFile + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	from, _ := syscall.UTF16PtrFromString(tmp)
	to, _ := syscall.UTF16PtrFromString(cacheFile)
	ok, _, callErr := moveFileEx.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), 0x1|0x8)
	if ok == 0 {
		_ = os.Remove(tmp)
		return callErr
	}
	return nil
}
