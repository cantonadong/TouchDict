package chatgpt

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

func (m *Manager) RefreshModels(ctx context.Context) ([]Model, error) {
	resp, err := m.request(ctx, "GET", "/models", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var catalog struct {
		Models []Model `json:"models"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&catalog) != nil {
		return nil, errors.New("订阅模型列表无效")
	}
	var models []Model
	for _, v := range catalog.Models {
		if v.Visibility == "list" && v.Slug != "" {
			if v.Name == "" {
				v.Name = v.Slug
			}
			models = append(models, v)
		}
	}
	if len(models) == 0 {
		return nil, errors.New("当前账号未返回可用订阅模型")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.saved
	s.Models = models
	if err = m.saveLocked(s); err != nil {
		return nil, err
	}
	return append([]Model(nil), models...), nil
}
func (m *Manager) Infer(ctx context.Context, name, instructions, input string) (string, error) {
	body, err := json.Marshal(map[string]any{"model": name, "instructions": instructions, "input": []map[string]string{{"role": "user", "content": input}}, "store": false, "stream": true})
	if err != nil {
		return "", err
	}
	resp, err := m.request(ctx, "POST", "/responses", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 4<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var output strings.Builder
	var data []string
	consume := func() (bool, error) {
		if len(data) == 0 {
			return false, nil
		}
		raw := strings.Join(data, "\n")
		data = nil
		if raw == "[DONE]" {
			return false, nil
		}
		var event struct {
			Type     string `json:"type"`
			Delta    string `json:"delta"`
			Response struct {
				Status string `json:"status"`
				Error  *struct {
					Code string `json:"code"`
				} `json:"error"`
				Output []struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"output"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(raw), &event) != nil {
			return false, errors.New("订阅响应格式无效")
		}
		switch event.Type {
		case "response.output_text.delta":
			output.WriteString(event.Delta)
		case "response.completed":
			if event.Response.Status != "completed" {
				return false, errors.New("订阅查询未完成")
			}
			if output.Len() == 0 {
				for _, item := range event.Response.Output {
					for _, content := range item.Content {
						if content.Type == "output_text" {
							output.WriteString(content.Text)
						}
					}
				}
			}
			return true, nil
		case "response.failed", "response.incomplete", "error":
			if event.Response.Error != nil && strings.Contains(event.Response.Error.Code, "usage") {
				return false, errors.New("订阅额度暂不可用，请稍后重试")
			}
			return false, errors.New("订阅查询失败或结果被截断，请重试")
		}
		return false, nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			done, e := consume()
			if e != nil {
				return "", e
			}
			if done {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				return output.String(), nil
			}
		} else if strings.HasPrefix(line, "data:") {
			data = append(data, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if scanner.Err() != nil {
		return "", errors.New("订阅响应读取失败，请重试")
	}
	done, err := consume()
	if err != nil {
		return "", err
	}
	if done {
		return output.String(), nil
	}
	return "", errors.New("订阅连接中断，未收到完整结果，请重试")
}
