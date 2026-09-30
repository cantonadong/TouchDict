package query

import (
	"context"
	"sync"
	"time"

	"touchdict/internal/gemini"
	"touchdict/internal/model"
)

type Service struct {
	mu            sync.Mutex
	clientFactory func() *gemini.Client
	timeout       time.Duration
	scopes        map[string]*scopeRequest
}

type scopeRequest struct {
	generation uint64
	cancel     context.CancelFunc
}

func New(clientFactory func() *gemini.Client, timeout time.Duration) *Service {
	return &Service{clientFactory: clientFactory, timeout: timeout, scopes: make(map[string]*scopeRequest)}
}

func (s *Service) Lookup(scope string, selection model.Selection, emit func(model.ViewState)) {
	s.mu.Lock()
	current := s.scopes[scope]
	if current != nil && current.cancel != nil {
		current.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.timeout)
	generation := uint64(1)
	if current != nil {
		generation = current.generation + 1
	}
	s.scopes[scope] = &scopeRequest{generation: generation, cancel: cancel}
	s.mu.Unlock()

	emit(model.ViewState{Kind: model.ViewLoading, Selection: selection.Text})
	go func() {
		result, err := s.clientFactory().Lookup(ctx, selection)
		if ctx.Err() != nil || !s.current(scope, generation) {
			return
		}
		if err != nil {
			emit(model.ViewState{Kind: model.ViewError, Selection: selection.Text, Message: err.Error(), CanRetry: true})
			return
		}
		if result.Definition != nil {
			emit(model.ViewState{Kind: model.ViewSuccess, Definition: *result.Definition})
			return
		}
		emit(model.ViewState{Kind: model.ViewSuggestions, Selection: selection.Text, Suggestions: result.Suggestions})
	}()
}

func (s *Service) current(scope string, generation uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.scopes[scope]
	return r != nil && r.generation == generation
}

func (s *Service) Cancel(scope string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.scopes[scope]; r != nil && r.cancel != nil {
		r.cancel()
	}
	delete(s.scopes, scope)
}

func (s *Service) History() []model.HistoryEntry { return gemini.History() }
func (s *Service) SubscribeHistory(fn func([]model.HistoryEntry)) func() {
	return gemini.SubscribeHistory(fn)
}
func (s *Service) SelectHistory(key string) (model.Definition, bool) {
	d, ok := gemini.HistoryDefinition(key)
	if ok {
		gemini.Touch(key)
	}
	return d, ok
}
