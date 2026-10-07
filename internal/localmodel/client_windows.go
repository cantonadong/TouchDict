//go:build windows

package localmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"touchdict/internal/gemini"
	"touchdict/internal/model"
)

type Client struct {
	runtime   *Runtime
	modelPath string
}

func New(runtime *Runtime, modelPath string) *Client {
	return &Client{runtime: runtime, modelPath: modelPath}
}
func (*Client) RequestTimeout() time.Duration { return 90 * time.Second }

func (c *Client) Lookup(ctx context.Context, selection model.Selection) (model.QueryResult, error) {
	if c.modelPath == "" {
		return model.QueryResult{}, errors.New("请在设置的本地标签中检索并选择模型")
	}
	if !selection.BypassCache {
		if definition, ok := gemini.Cached(selection); ok {
			return model.QueryResult{Definition: &definition}, nil
		}
	}
	select {
	case c.runtime.gate <- struct{}{}:
		defer func() { <-c.runtime.gate }()
	case <-ctx.Done():
		return model.QueryResult{}, localError(ctx.Err())
	}
	if err := ctx.Err(); err != nil {
		return model.QueryResult{}, localError(err)
	}
	server, err := c.runtime.start(ctx, c.modelPath)
	if err != nil {
		return model.QueryResult{}, err
	}
	if err = c.runtime.ready(ctx, server); err != nil {
		c.runtime.mu.Lock()
		if c.runtime.server == server {
			c.runtime.stopLocked()
		}
		c.runtime.mu.Unlock()
		return model.QueryResult{}, localError(err)
	}
	properties := map[string]any{}
	for _, field := range []string{"kind", "term", "partOfSpeech", "meaningZh", "exampleEn", "exampleZh"} {
		properties[field] = map[string]any{"type": "string", "minLength": 1}
	}
	properties["type"] = map[string]any{"type": "string", "enum": []string{"definition"}}
	properties["term"] = map[string]any{"type": "string", "enum": []string{trim(selection.Text, 300)}}
	properties["kind"] = map[string]any{"type": "string", "enum": []string{"term", "sentence"}}
	properties["suggestions"] = map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	schema := map[string]any{"type": "object", "properties": properties, "required": []string{"type", "kind", "term", "partOfSpeech", "meaningZh", "exampleEn", "exampleZh", "suggestions"}, "additionalProperties": false}
	// A single user turn works with both Gemma and Qwen chat templates.
	prompt := model.ContextualDictionaryInstructions + "\n\nReturn one JSON object matching the schema. All six string fields must be nonempty: term is exactly selection; partOfSpeech is an abbreviation (n., v., adj., adv., phr.; sent. for a selected sentence); meaningZh is the selected term's Chinese meaning; exampleEn is one SHORT natural English example using the selected term in this sense; exampleZh is its accurate Chinese translation. type=definition; kind=term for a word or phrase, kind=sentence only for a complete selected sentence; suggestions=[]. For a selected sentence, still provide an English example and Chinese translation.\n<selection>" + escape(trim(selection.Text, 300)) + "</selection>\n<context>" + escape(trim(selection.Context, 1200)) + "</context>"
	payload := map[string]any{"messages": []map[string]string{{"role": "user", "content": prompt}}, "temperature": 0.2, "max_tokens": 768, "stream": false,
		"response_format": map[string]any{"type": "json_object", "schema": schema}}
	data, err := json.Marshal(payload)
	if err != nil {
		return model.QueryResult{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.url+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return model.QueryResult{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+server.token)
	response, err := c.runtime.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return model.QueryResult{}, localError(ctx.Err())
		}
		return model.QueryResult{}, errors.New("无法连接本地模型，请重新查询或在设置中切换模型")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return model.QueryResult{}, localError(err)
	}
	if response.StatusCode != http.StatusOK {
		return model.QueryResult{}, fmt.Errorf("本地模型查询失败（%d），请查看 local-model.log", response.StatusCode)
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Choices) == 0 {
		return model.QueryResult{}, errors.New("本地模型没有返回有效结果")
	}
	if envelope.Choices[0].FinishReason == "length" {
		return model.QueryResult{}, errors.New("本地模型结果被截断，请重试")
	}
	var answer struct {
		Type string `json:"type"`
		model.Definition
		Suggestions []string `json:"suggestions"`
	}
	if json.Unmarshal([]byte(envelope.Choices[0].Message.Content), &answer) != nil {
		return model.QueryResult{}, errors.New("本地模型返回的词典格式无法解析，请重试")
	}
	if answer.Type == "suggestions" {
		var suggestions []string
		seen := make(map[string]bool)
		for _, value := range answer.Suggestions {
			value = trim(value, 80)
			key := strings.ToLower(value)
			if value != "" && !seen[key] {
				suggestions = append(suggestions, value)
				seen[key] = true
			}
			if len(suggestions) == 5 {
				break
			}
		}
		if len(suggestions) == 0 {
			return model.QueryResult{}, errors.New("本地模型未返回有效的拼写建议")
		}
		return model.QueryResult{Suggestions: suggestions}, nil
	}
	d := answer.Definition
	d.Term, d.Kind, d.PartOfSpeech = trim(d.Term, 300), trim(d.Kind, 20), trim(d.PartOfSpeech, 30)
	d.MeaningZH, d.ExampleEN, d.ExampleZH = trim(d.MeaningZH, 500), trim(d.ExampleEN, 500), trim(d.ExampleZH, 500)
	if answer.Type != "definition" || d.Term == "" || d.MeaningZH == "" || d.PartOfSpeech == "" || d.ExampleEN == "" || d.ExampleZH == "" {
		return model.QueryResult{}, errors.New("本地模型返回的释义不完整，请重试或切换模型")
	}
	if !model.DefinitionMatchesSelection(selection, d) {
		return model.QueryResult{}, errors.New("本地模型返回的内容与所查词不符，请重试或切换模型")
	}
	if err := ctx.Err(); err != nil {
		return model.QueryResult{}, localError(err)
	}
	gemini.StoreDefinition(selection, d)
	return model.QueryResult{Definition: &d}, nil
}

func localError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("本地模型加载或查询超时，请重试")
	}
	return err
}

func trim(value string, limit int) string {
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return strings.TrimSpace(value)
}

func escape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(value)
}
