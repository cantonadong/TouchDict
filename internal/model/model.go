package model

type Selection struct {
	Text      string
	Context   string
	Source    string
	Multiword bool
}

type Definition struct {
	Kind         string `json:"kind"`
	Term         string `json:"term"`
	PartOfSpeech string `json:"partOfSpeech"`
	MeaningZH    string `json:"meaningZh"`
	ExampleEN    string `json:"exampleEn"`
	ExampleZH    string `json:"exampleZh"`
}

type ViewKind int

const (
	ViewHidden ViewKind = iota
	ViewLoading
	ViewSuccess
	ViewEmpty
	ViewError
)

type ViewState struct {
	Kind       ViewKind
	Selection  string
	Definition Definition
	Message    string
	CanRetry   bool
}
