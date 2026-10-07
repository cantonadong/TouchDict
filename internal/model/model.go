package model

type Selection struct {
	BypassCache bool
	Text        string
	Context     string
	Source      string
	Multiword   bool
	Bounds      *SelectionBounds
}

type SelectionBounds struct{ Left, Top, Right, Bottom int }

type Definition struct {
	Kind         string `json:"kind"`
	Term         string `json:"term"`
	PartOfSpeech string `json:"partOfSpeech"`
	MeaningZH    string `json:"meaningZh"`
	ExampleEN    string `json:"exampleEn"`
	ExampleZH    string `json:"exampleZh"`
}

type QueryResult struct {
	Definition  *Definition
	Suggestions []string
}

type HistoryEntry struct {
	Key        string
	Query      string
	Context    string
	Definition Definition
	Count      uint64
}

type ViewKind int

const (
	ViewHidden ViewKind = iota
	ViewLoading
	ViewSuccess
	ViewEmpty
	ViewError
	ViewSuggestions
)

type ViewState struct {
	Context     string
	Kind        ViewKind
	Selection   string
	Definition  Definition
	Message     string
	CanRetry    bool
	Suggestions []string
}
