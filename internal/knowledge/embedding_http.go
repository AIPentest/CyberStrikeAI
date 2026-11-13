package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type httpTextEmbedder struct {
	client               *http.Client
	endpoint, key, model string
}

func (e *httpTextEmbedder) EmbedStrings(ctx context.Context, texts []string, _ ...EmbeddingOption) ([][]float64, error) {
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts, "encoding_format": "float"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.key)
	res, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return nil, fmt.Errorf("embedding HTTP %d: %s", res.StatusCode, b)
	}
	var output struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 64*1024*1024)).Decode(&output); err != nil {
		return nil, err
	}
	if len(output.Data) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: %d vs %d", len(output.Data), len(texts))
	}
	vectors := make([][]float64, len(texts))
	for _, row := range output.Data {
		if row.Index < 0 || row.Index >= len(vectors) || vectors[row.Index] != nil || len(row.Embedding) == 0 {
			return nil, fmt.Errorf("invalid embedding response index or vector")
		}
		vectors[row.Index] = row.Embedding
	}
	return vectors, nil
}
