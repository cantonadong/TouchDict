package online

import (
	"context"
	"errors"
	"touchdict/internal/chatgpt"
	"touchdict/internal/gemini"
	"touchdict/internal/model"
)

type subscriptionClient struct{ name string }

func (c *subscriptionClient) Lookup(ctx context.Context, selection model.Selection) (model.QueryResult, error) {
	if !chatgpt.Default.Connected() {
		return model.QueryResult{}, errors.New("请在设置的订阅标签中点击授权")
	}
	if !selection.BypassCache {
		if cached, ok := gemini.Cached(selection); ok {
			return model.QueryResult{Definition: &cached}, nil
		}
	}
	instructions := model.ContextualDictionaryInstructions + "\n" + model.LearningInstructions + "\n" + model.PhoneticInstructions + "\n" + dictionaryFormat
	input := "<selection>" + escape(limit(selection.Text, 300)) + "</selection>\n<context>" + escape(limit(selection.Context, 1200)) + "</context>"
	content, err := chatgpt.Default.Infer(ctx, c.name, instructions, input)
	if err != nil {
		return model.QueryResult{}, err
	}
	return parseDictionary(ctx, selection, content, "ChatGPT 订阅", false)
}
