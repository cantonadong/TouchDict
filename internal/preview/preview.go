package preview

import "touchdict/internal/model"

func State(name string) model.ViewState {
	switch name {
	case "loading":
		return model.ViewState{Kind: model.ViewLoading, Selection: "serendipity"}
	case "empty":
		return model.ViewState{Kind: model.ViewEmpty, Message: "没有读取到选中的英文文本"}
	case "error":
		return model.ViewState{Kind: model.ViewError, Selection: "context", Message: "无法连接 Gemini，请检查网络后重试", CanRetry: true}
	case "edge":
		return model.ViewState{Kind: model.ViewSuccess, Definition: model.Definition{Term: "a surprisingly long selected expression", PartOfSpeech: "phr.", MeaningZH: "在特定上下文中使用的较长表达方式，用于预览自动换行和窗口边界。", ExampleEN: "She used a surprisingly long selected expression to explain the idea clearly.", ExampleZH: "她使用了一个较长的表达方式来清楚地解释这个想法。"}}
	default:
		return model.ViewState{Kind: model.ViewSuccess, Definition: model.Definition{Term: "subtle", PartOfSpeech: "adj.", MeaningZH: "细微的；不易察觉的", ExampleEN: "There is a subtle difference between the two colors.", ExampleZH: "这两种颜色之间有细微的差别。"}}
	}
}
