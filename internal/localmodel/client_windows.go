//go:build windows

package localmodel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

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
		if definition, ok := gemini.Cached(selection); ok && model.ValidateDictionaryDefinition(selection, definition) == nil {
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
	feedback := ""
	for attempt := 0; attempt < 2; attempt++ {
		result, err := c.query(ctx, server, selection, attempt, feedback)
		if err == nil || !errors.Is(err, errInvalidDefinition) {
			return result, err
		}
		if attempt == 1 {
			return model.QueryResult{}, fmt.Errorf("本地模型生成结果不合格：%v", err)
		}
		feedback = err.Error()
	}
	return model.QueryResult{}, errInvalidDefinition
}

var errInvalidDefinition = errors.New("本地模型返回的释义未通过校验")

func (c *Client) query(ctx context.Context, server *serverProcess, selection model.Selection, attempt int, feedback string) (model.QueryResult, error) {
	query := trim(selection.Text, 300)
	// Gemma accepts a single user turn; instruction and selection text remain
	// identical to Gemini's system and user instructions.
	prompt := model.DictionarySystemInstructions + "\n\n" + model.DictionarySelectionPrompt(selection)
	if feedback != "" {
		prompt += "\n上次生成结果的校验错误：" + feedback + "。请修正该问题并重新生成完整 JSON，所有基础要求保持不变。"
	}
	// Render the model's own chat template, then use native completion with
	// a non-lazy grammar. Chat parsing must not gate dictionary JSON sampling.
	templateRaw, err := c.post(ctx, server, "/apply-template", map[string]any{
		"messages":              []map[string]string{{"role": "user", "content": prompt}},
		"add_generation_prompt": true,
	})
	if err != nil {
		return model.QueryResult{}, err
	}
	var rendered struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal(templateRaw, &rendered) != nil || rendered.Prompt == "" {
		return model.QueryResult{}, errors.New("本地模型无法生成查询模板")
	}
	payload := map[string]any{"prompt": rendered.Prompt, "temperature": model.DictionaryTemperature, "n_predict": model.DictionaryMaxOutputTokens, "stream": false, "grammar": dictionaryGrammar(query), "grammar_lazy": false}
	raw, err := c.post(ctx, server, "/completion", payload)
	if err != nil {
		return model.QueryResult{}, err
	}
	var envelope struct {
		Content            string `json:"content"`
		StopType           string `json:"stop_type"`
		GenerationSettings struct {
			Grammar     string `json:"grammar"`
			GrammarLazy bool   `json:"grammar_lazy"`
		} `json:"generation_settings"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Content == "" {
		return model.QueryResult{}, errors.New("本地模型没有返回有效结果")
	}
	if envelope.StopType == "limit" {
		return model.QueryResult{}, errors.New("本地模型结果被截断，请重试")
	}
	log.Printf("local dictionary grammar: enabled=%t lazy=%t", envelope.GenerationSettings.Grammar != "", envelope.GenerationSettings.GrammarLazy)
	var answer struct {
		Type string `json:"type"`
		model.Definition
		Suggestions []string `json:"suggestions"`
	}
	if err := decodeDefinition(envelope.Content, &answer); err != nil {
		log.Printf("local dictionary JSON parsing: attempt=%d error=%v content=%q", attempt+1, err, trim(envelope.Content, 6000))
		return model.QueryResult{}, fmt.Errorf("%w：词典格式无法解析", errInvalidDefinition)
	}
	if answer.Type == "suggestions" {
		seen := make(map[string]bool)
		var suggestions []string
		for _, value := range answer.Suggestions {
			value = trim(value, 80)
			key := strings.ToLower(value)
			if value != "" && !seen[key] {
				seen[key] = true
				suggestions = append(suggestions, value)
			}
			if len(suggestions) == 5 {
				break
			}
		}
		if len(suggestions) == 0 {
			return model.QueryResult{}, fmt.Errorf("%w：拼写建议为空", errInvalidDefinition)
		}
		return model.QueryResult{Suggestions: suggestions}, nil
	}
	d := answer.Definition
	if answer.Type == "definition" && d.Kind == "term" && d.LearningZH != "" && d.ExampleZH != "" && !model.DefinitionHasLearning(d) {
		match, err := c.repairHighlight(ctx, server, selection, d)
		if err != nil {
			return model.QueryResult{}, err
		}
		d.ExampleZHMatch = match
	}
	return c.accept(ctx, selection, answer.Type, d, attempt)
}

// Repair alignment using only literal substrings of the existing translation;
// preserve the definition and example instead of asking for a new answer.
func (c *Client) repairHighlight(ctx context.Context, server *serverProcess, selection model.Selection, d model.Definition) (string, error) {
	var choices []string
	seen := make(map[string]bool)
	runes := []rune(trim(d.ExampleZH, 500))
	for start, r := range runes {
		if !unicode.Is(unicode.Han, r) {
			continue
		}
		for end := start; end < len(runes) && end < start+12; end++ {
			if !unicode.Is(unicode.Han, runes[end]) {
				break
			}
			value := string(runes[start : end+1])
			if !seen[value] {
				seen[value] = true
				encoded, _ := json.Marshal(value)
				choices = append(choices, strconv.Quote(string(encoded)))
			}
		}
	}
	if len(choices) == 0 {
		return "", fmt.Errorf("%w：例句翻译缺少中文，无法定位高亮", errInvalidDefinition)
	}
	input, _ := json.Marshal(map[string]string{"term": selection.Text, "exampleEn": d.ExampleEN, "exampleZh": d.ExampleZH})
	prompt := "找出中文译文中与所查英文词对应的最小连续原文片段。只能从 exampleZh 原文复制，不能改写，不能选择整句或无关词。只返回 JSON 对象，字段 exampleZhMatch。\n" + string(input)
	templateRaw, err := c.post(ctx, server, "/apply-template", map[string]any{"messages": []map[string]string{{"role": "user", "content": prompt}}, "add_generation_prompt": true})
	if err != nil {
		return "", err
	}
	var rendered struct {
		Prompt string `json:"prompt"`
	}
	if json.Unmarshal(templateRaw, &rendered) != nil || rendered.Prompt == "" {
		return "", errors.New("本地模型无法生成高亮修复模板")
	}
	grammar := `root ::= "{" ws "\"exampleZhMatch\"" ws ":" ws match ws "}" ws` + "\nmatch ::= " + strings.Join(choices, " | ") + "\n" + `ws ::= [ \t\r\n]{0,8}` + "\n"
	raw, err := c.post(ctx, server, "/completion", map[string]any{"prompt": rendered.Prompt, "temperature": model.DictionaryTemperature, "n_predict": 128, "stream": false, "grammar": grammar, "grammar_lazy": false})
	if err != nil {
		return "", err
	}
	var envelope struct {
		Content string `json:"content"`
	}
	var answer struct {
		Match string `json:"exampleZhMatch"`
	}
	if json.Unmarshal(raw, &envelope) != nil || decodeDefinition(envelope.Content, &answer) != nil || answer.Match == "" || !strings.Contains(d.ExampleZH, answer.Match) {
		return "", fmt.Errorf("%w：无法定位中文例句高亮", errInvalidDefinition)
	}
	log.Printf("local dictionary highlight: repaired=true")
	return answer.Match, nil
}

// Use explicit GBNF instead of relying on runtime-specific schema conversion.
// Fixed structural fields precede free text, keeping small models on the query.
func dictionaryGrammar(query string) string {
	jsonLiteral := func(value string) string {
		encoded, _ := json.Marshal(value)
		return strconv.Quote(string(encoded))
	}
	object := func(answerType, kind string) string {
		fields := []string{jsonLiteral("type") + ` ws ":" ws ` + jsonLiteral(answerType)}
		names := []string{"kind", "term", "partOfSpeech", "phonetic", "meaningZh", "learningZh", "exampleEn", "exampleZh", "exampleZhMatch"}
		for _, name := range names {
			rule := "string"
			if name == "learningZh" {
				rule = "learning-string"
			} else if name == "exampleEn" {
				rule = "example-string"
			} else if name == "meaningZh" || name == "exampleZh" {
				rule = "chinese-string"
			} else if name == "partOfSpeech" || name == "exampleZhMatch" {
				rule = "nonempty-string"
			} else if name == "phonetic" {
				if len(strings.Fields(query)) == 1 {
					rule = "phonetic-string"
				} else {
					rule = jsonLiteral("")
				}
			}
			if answerType == "suggestions" {
				rule = jsonLiteral("")
			} else if name == "kind" {
				rule = jsonLiteral(kind)
			} else if name == "term" {
				rule = jsonLiteral(query)
			} else if kind == "sentence" && (name == "partOfSpeech" || name == "phonetic" || name == "exampleZhMatch" || name == "exampleEn" || name == "exampleZh") {
				rule = jsonLiteral("")
			}
			fields = append(fields, jsonLiteral(name)+` ws ":" ws `+rule)
		}
		rule := `"[]"`
		if answerType == "suggestions" {
			rule = `"[" ws string (ws "," ws string){0,4} ws "]"`
		}
		fields = append(fields, jsonLiteral("suggestions")+` ws ":" ws `+rule)
		return `"{" ws ` + strings.Join(fields, ` ws "," ws `) + ` ws "}" ws`
	}
	root := "term-answer | suggestions-answer"
	if len(strings.Fields(query)) > 1 {
		root += " | sentence-answer"
	}
	encodedQuery, _ := json.Marshal(query)
	// The literal is the JSON-escaped content, excluding its quote delimiters.
	// Both surrounding separators must be word boundaries accepted by the
	// shared validator; inflected replacements cannot satisfy this grammar.
	queryLiteral := strconv.Quote(string(encodedQuery[1 : len(encodedQuery)-1]))
	return "root ::= " + root + "\n" +
		"term-answer ::= " + object("definition", "term") + "\n" +
		"sentence-answer ::= " + object("definition", "sentence") + "\n" +
		"suggestions-answer ::= " + object("suggestions", "") + "\n" +
		`string ::= "\"" char{0,500} "\""` + "\n" +
		`nonempty-string ::= "\"" char{1,500} "\""` + "\n" +
		`chinese-string ::= "\"" [\u3400-\u9FFF] char{0,499} "\""` + "\n" +
		`learning-string ::= "\"" [\u3400-\u9FFF] char{0,799} "\""` + "\n" +
		`phonetic-string ::= "\"/" ipa-char{1,118} "/\""` + "\n" +
		`ipa-char ::= [^"\\/\x00-\x1F]` + "\n" +
		`example-string ::= "\"" (english-char{0,120} boundary)? ` + queryLiteral + ` (boundary english-char{0,120})? "\""` + "\n" +
		`english-char ::= [A-Za-z0-9 .,!?;:()'/-]` + "\n" +
		`boundary ::= [ .,!?;:()'/-]` + "\n" +
		`char ::= [^"\\\x00-\x1F] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F]{4})` + "\n" +
		`ws ::= [ \t\n\r]{0,8}` + "\n"
}
func (c *Client) post(ctx context.Context, server *serverProcess, endpoint string, payload map[string]any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, server.url+endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+server.token)
	response, err := c.runtime.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, localError(ctx.Err())
		}
		return nil, errors.New("无法连接本地模型，请重新查询或在设置中切换模型")
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024))
	if err != nil {
		return nil, localError(err)
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(raw, &failure) == nil && failure.Error.Message != "" {
			log.Printf("local dictionary server error: status=%d message=%q", response.StatusCode, trim(strings.ReplaceAll(failure.Error.Message, server.token, "[redacted]"), 800))
		}
		return nil, fmt.Errorf("本地模型查询失败（%d），请查看 local-model.log", response.StatusCode)
	}
	return raw, nil
}

// Accept harmless Markdown/prose wrapping, but never repair malformed JSON.
func decodeDefinition(content string, target any) error {
	content = strings.TrimSpace(content)
	if json.Valid([]byte(content)) {
		return json.Unmarshal([]byte(content), target)
	}
	start := strings.IndexByte(content, '{')
	if start < 0 {
		return errors.New("no JSON object")
	}
	decoder := json.NewDecoder(strings.NewReader(content[start:]))
	return decoder.Decode(target)
}

func (c *Client) accept(ctx context.Context, selection model.Selection, answerType string, d model.Definition, attempt int) (model.QueryResult, error) {
	d.LearningZH = trim(d.LearningZH, 800)
	d.ExampleZHMatch = trim(d.ExampleZHMatch, 500)
	d.Term, d.Kind, d.PartOfSpeech = trim(d.Term, 300), trim(d.Kind, 20), trim(d.PartOfSpeech, 30)
	model.NormalizePhonetic(selection, &d)
	d.MeaningZH, d.ExampleEN, d.ExampleZH = trim(d.MeaningZH, 500), trim(d.ExampleEN, 500), trim(d.ExampleZH, 500)
	if answerType != "definition" {
		return model.QueryResult{}, fmt.Errorf("%w：结果类型无效", errInvalidDefinition)
	}
	if err := model.ValidateDictionaryDefinition(selection, d); err != nil {
		log.Printf("local dictionary validation: attempt=%d error=%v term_match=%t kind=%q example=%q", attempt+1, err, strings.EqualFold(strings.TrimSpace(selection.Text), d.Term), d.Kind, d.ExampleEN)
		return model.QueryResult{}, fmt.Errorf("%w：%v", errInvalidDefinition, err)
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
