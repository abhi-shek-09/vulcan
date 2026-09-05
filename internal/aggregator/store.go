package aggregator

import "sync"

type Store struct {
	mu sync.RWMutex

	tests map[string]*GlobalWindow
}

func NewStore() *Store {

	return &Store{
		tests: make(map[string]*GlobalWindow),
	}
}

func (s *Store) Get(testID string) *GlobalWindow {

	s.mu.RLock()
	window, ok := s.tests[testID]
	s.mu.RUnlock()

	if ok {
		return window
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	window, ok = s.tests[testID]
	if ok {
		return window
	}

	window = NewGlobalWindow(testID)

	s.tests[testID] = window

	return window
}
