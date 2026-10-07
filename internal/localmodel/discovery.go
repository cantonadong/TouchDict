package localmodel

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Model struct {
	Path string
	Name string
}

func Discover(directory string) ([]Model, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("请输入本地模型目录")
	}
	root, err := filepath.Abs(directory)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("本地模型目录不存在或无法读取：%s", directory)
	}
	var models []Model
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && strings.HasPrefix(entry.Name(), ".") {
			return filepath.SkipDir
		}
		if !entry.IsDir() && entry.Type().IsRegular() && strings.EqualFold(filepath.Ext(path), ".gguf") {
			relative, _ := filepath.Rel(root, path)
			models = append(models, Model{Path: path, Name: relative})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("检索本地模型失败：%w", err)
	}
	sort.Slice(models, func(i, j int) bool { return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name) })
	return models, nil
}
