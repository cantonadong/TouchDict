package online

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

type Client interface {
	Lookup(context.Context, model.Selection) (model.QueryResult, error)
}

func Provider(name string) string {
	if strings.HasPrefix(name, "chatgpt:") {
		return "ChatGPT 订阅"
	}
	if strings.HasPrefix(name, "groq:") {
		return "Groq"
	}
	if strings.HasPrefix(name, "openrouter:") {
		return "OpenRouter"
	}
	return "Gemini"
}

func Label(name string) string {
	if _, id, ok := strings.Cut(name, ":"); ok {
		return Provider(name) + " · " + id
	}
	return name
}

func New(key, name string) Client {
	if strings.HasPrefix(name, "chatgpt:") {
		return &subscriptionClient{name: strings.TrimPrefix(name, "chatgpt:")}
	}
	if strings.HasPrefix(name, "groq:") {
		return &compatibleClient{key: strings.TrimSpace(key), name: strings.TrimPrefix(name, "groq:"), provider: "Groq", endpoint: "https://api.groq.com/openai/v1/chat/completions"}
	}
	if strings.HasPrefix(name, "openrouter:") {
		return &compatibleClient{key: strings.TrimSpace(key), name: strings.TrimPrefix(name, "openrouter:"), provider: "OpenRouter", endpoint: "https://openrouter.ai/api/v1/chat/completions"}
	}
	return gemini.New(key, name)
}

type compatibleClient struct {
	key, name, provider, endpoint string
	skipHistory                   bool
}

const dictionaryFormat = `Return exactly one JSON object with fields type, kind, term, partOfSpeech, phonetic, meaningZh, learningZh, exampleZhMatch, exampleEn, exampleZh, suggestions. type=definition for a valid word, phrase or sentence, type=suggestions for likely spelling errors. For suggestions return up to five English corrections and empty strings for all definition fields. kind=term for a word or phrase, kind=sentence only for a complete sentence. term must exactly equal selection. partOfSpeech is an abbreviation such as n., v., adj., adv., phr. For sentences return empty partOfSpeech, exampleEn, exampleZh and exampleZhMatch. For terms supply a short English example containing the exact selection and its Chinese translation. Use American English. Output JSON only, never Markdown.`

func (c *compatibleClient) Lookup(ctx context.Context, selection model.Selection) (model.QueryResult, error) {
	if c.key == "" {
		return model.QueryResult{}, fmt.Errorf("请在设置中填写 %s API Key", c.provider)
	}
	if c.provider == "OpenRouter" && c.name != "openrouter/free" {
		return model.QueryResult{}, errors.New("OpenRouter 仅允许免费模型路由")
	}
	if !selection.BypassCache {
		if cached, ok := gemini.Cached(selection); ok {
			return model.QueryResult{Definition: &cached}, nil
		}
	}
	system := model.ContextualDictionaryInstructions + "\n" + model.LearningInstructions + "\n" + model.PhoneticInstructions + "\n" + dictionaryFormat
	payload := map[string]any{
		"model":       c.name,
		"messages":    []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": "<selection>" + escape(limit(selection.Text, 300)) + "</selection>\n<context>" + escape(limit(selection.Context, 1200)) + "</context>"}},
		"temperature": 0.2, "max_tokens": 2048, "stream": false, "response_format": map[string]string{"type": "json_object"},
	}
	if strings.HasPrefix(c.name, "openai/gpt-oss-") {
		payload["reasoning_effort"] = "low"
	}
	if c.provider == "OpenRouter" {
		payload["provider"] = map[string]any{"require_parameters": true}
		payload["reasoning"] = map[string]any{"effort": "low", "exclude": true}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return model.QueryResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return model.QueryResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 18 * time.Second}).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return model.QueryResult{}, ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return model.QueryResult{}, fmt.Errorf("%s 查询超时", c.provider)
		}
		return model.QueryResult{}, fmt.Errorf("无法连接 %s，请检查网络", c.provider)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return model.QueryResult{}, fmt.Errorf("%s 响应读取失败", c.provider)
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return model.QueryResult{}, fmt.Errorf("%s 响应格式无效（HTTP %d）", c.provider, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || response.Error != nil {
		message := "服务请求失败"
		if response.Error != nil {
			message = limit(strings.ReplaceAll(response.Error.Message, c.key, "[redacted]"), 500)
		}
		return model.QueryResult{}, fmt.Errorf("%s（HTTP %d）：%s", c.provider, resp.StatusCode, message)
	}
	if len(response.Choices) == 0 {
		return model.QueryResult{}, fmt.Errorf("%s 未返回查词结果", c.provider)
	}
	choice := response.Choices[0]
	if choice.FinishReason == "length" {
		return model.QueryResult{}, fmt.Errorf("%s 结果被截断，请重试", c.provider)
	}
	content := strings.TrimSpace(choice.Message.Content)
	return parseDictionary(ctx, selection, content, c.provider, c.skipHistory)
}

func parseDictionary(ctx context.Context, selection model.Selection, content, provider string, skipHistory bool) (model.QueryResult, error) {
	if strings.HasPrefix(content, "```") {
		if i := strings.IndexByte(content, '\n'); i >= 0 {
			content = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(content[i+1:]), "```"))
		}
	}
	var answer struct {
		Type string `json:"type"`
		model.Definition
		Suggestions []string `json:"suggestions"`
	}
	if json.Unmarshal([]byte(content), &answer) != nil {
		return model.QueryResult{}, fmt.Errorf("%s 查词结果 JSON 无法解析", provider)
	}
	if answer.Type == "suggestions" {
		seen := map[string]bool{}
		var values []string
		for _, value := range answer.Suggestions {
			value = limit(value, 80)
			id := strings.ToLower(value)
			if value != "" && !seen[id] {
				seen[id] = true
				values = append(values, value)
			}
			if len(values) == 5 {
				break
			}
		}
		if len(values) == 0 {
			return model.QueryResult{}, fmt.Errorf("%s 未返回有效拼写建议", provider)
		}
		return model.QueryResult{Suggestions: values}, nil
	}
	d := answer.Definition
	d.Term, d.Kind, d.PartOfSpeech = limit(d.Term, 300), limit(d.Kind, 20), limit(d.PartOfSpeech, 30)
	d.MeaningZH, d.LearningZH = limit(d.MeaningZH, 500), limit(d.LearningZH, 800)
	d.ExampleEN, d.ExampleZH, d.ExampleZHMatch = limit(d.ExampleEN, 500), limit(d.ExampleZH, 500), limit(d.ExampleZHMatch, 500)
	model.NormalizePhonetic(selection, &d)
	if answer.Type != "definition" || d.MeaningZH == "" || !model.DefinitionHasLearning(d) || !model.DefinitionHasPhonetic(selection, d) || !model.DefinitionMatchesSelection(selection, d) {
		return model.QueryResult{}, fmt.Errorf("%s 查词结果不完整或与所查词不符，请重试", provider)
	}
	if d.Kind != "sentence" && (d.PartOfSpeech == "" || d.ExampleEN == "" || d.ExampleZH == "") {
		return model.QueryResult{}, fmt.Errorf("%s 查词结果缺少词性或例句，请重试", provider)
	}
	if ctx.Err() != nil {
		return model.QueryResult{}, ctx.Err()
	}
	if !skipHistory {
		gemini.StoreDefinition(selection, d)
	}
	return model.QueryResult{Definition: &d}, nil
}

func MeasureQuery(ctx context.Context, key, name string) (time.Duration, error) {
	if Provider(name) == "ChatGPT 订阅" {
		return 0, errors.New("订阅模型请通过实际查词使用，全部在线模型测速会跳过订阅")
	}
	if Provider(name) == "Gemini" {
		return gemini.MeasureQuery(ctx, key, name)
	}
	client := New(key, name).(*compatibleClient)
	client.skipHistory = true
	start := time.Now()
	_, err := client.Lookup(ctx, model.Selection{Text: "hello", BypassCache: true})
	return time.Since(start), err
}

func limit(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
