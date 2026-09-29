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
	Definition model.Definition `json:"definition"`
}

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
		resultCache.order = append(resultCache.order, entry.Key)
	}
	return saveCacheLocked()
}

func storeCached(key string, definition model.Definition) {
	resultCache.Lock()
	defer resultCache.Unlock()
	removeOrderKeyLocked(key)
	resultCache.items[key] = definition
	resultCache.order = append(resultCache.order, key)
	for len(resultCache.order) > cacheLimit {
		delete(resultCache.items, resultCache.order[0])
		resultCache.order = resultCache.order[1:]
	}
	_ = saveCacheLocked()
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
		saved.Entries = append(saved.Entries, diskCacheEntry{Key: key, Definition: resultCache.items[key]})
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
