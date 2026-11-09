package agent

import (
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

// TokenCounter estimates the token count of text (tiktoken; falls back to cl100k_base when the model is unknown).
type TokenCounter interface {
	Count(model, text string) (int, error)
}

type tikTokenCounter struct {
	mu    sync.Mutex
	cache map[string]*tiktoken.Tiktoken
}

// NewTikTokenCounter creates a tiktoken-based TokenCounter.
func NewTikTokenCounter() TokenCounter {
	return &tikTokenCounter{cache: make(map[string]*tiktoken.Tiktoken)}
}

func (c *tikTokenCounter) encoding(model string) (*tiktoken.Tiktoken, error) {
	key := model
	if key == "" {
		key = "cl100k_base"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if enc, ok := c.cache[key]; ok {
		return enc, nil
	}
	enc, err := tiktoken.EncodingForModel(key)
	if err != nil {
		enc, err = tiktoken.GetEncoding("cl100k_base")
	}
	if err != nil {
		return nil, err
	}
	c.cache[key] = enc
	return enc, nil
}

func (c *tikTokenCounter) Count(model, text string) (int, error) {
	if text == "" {
		return 0, nil
	}
	enc, err := c.encoding(model)
	if err != nil {
		return 0, err
	}
	return len(enc.Encode(text, nil, nil)), nil
}
