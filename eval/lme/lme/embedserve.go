package lme

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// OpenAI-compatible embedding server (POST /v1/embeddings, GET /v1/models)
// backed by a Chanda embedder, so the Mem0 condition embeds with the very
// model implementation (and model id) Chanda uses.
//
// Prefix (role). The OpenAI request has no notion of query versus stored
// text, so the role is chosen, in order, by: the non-standard request field
// "input_type" ("query" | "passage"; also "search_query" | "search_document"
// as in other providers), a ":query" or ":passage" suffix on the model name,
// and otherwise the server's -role flag (default "query"). With e5 the role
// is the "query: " / "passage: " prefix; the hash embedder has no prefix.
// The default is "query" because e5's model card asks for "query: " on both
// sides of symmetric tasks, and a client that cannot tell the two apart
// (Mem0 embeds memories and searches with the same call) is symmetric.

// EmbeddingRequest is the OpenAI request body (plus input_type).
type EmbeddingRequest struct {
	Input          json.RawMessage `json:"input"`
	Model          string          `json:"model"`
	EncodingFormat string          `json:"encoding_format,omitempty"` // "float" (default) or "base64"
	Dimensions     int             `json:"dimensions,omitempty"`
	User           string          `json:"user,omitempty"`
	InputType      string          `json:"input_type,omitempty"`
}

// EmbeddingData is one vector of the response; Embedding is []float32 or a
// base64 string.
type EmbeddingData struct {
	Object    string `json:"object"`
	Index     int    `json:"index"`
	Embedding any    `json:"embedding"`
}

// EmbeddingResponse is the OpenAI response body.
type EmbeddingResponse struct {
	Object string          `json:"object"`
	Data   []EmbeddingData `json:"data"`
	Model  string          `json:"model"`
	Usage  struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

// ModelInfo is one entry of GET /v1/models (dim and role are extensions).
type ModelInfo struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Dim         int    `json:"dim"`
	DefaultRole string `json:"default_role"`
}

type apiError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

// EmbedServer serves an embedder over the OpenAI embeddings API.
type EmbedServer struct {
	Emb         *Cached
	DefaultRole string
	MaxInputs   int // texts per request (default 2048, OpenAI's limit)
	mu          sync.Mutex
	requests    int64
	texts       int64
}

// Handler returns the HTTP handler.
func (s *EmbedServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/embeddings", s.embeddings)
	mux.HandleFunc("POST /embeddings", s.embeddings)
	mux.HandleFunc("GET /v1/models", s.models)
	mux.HandleFunc("GET /models", s.models)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "ok\n") })
	return mux
}

func (s *EmbedServer) role() string {
	if s.DefaultRole == RolePassage {
		return RolePassage
	}
	return RoleQuery
}

func (s *EmbedServer) models(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []ModelInfo{{
		ID: s.Emb.ModelID(), Object: "model", OwnedBy: "chanda", Dim: s.Emb.Dim(), DefaultRole: s.role(),
	}}})
}

func fail(w http.ResponseWriter, status int, typ, msg string) {
	var e apiError
	e.Error.Message, e.Error.Type = msg, typ
	writeJSON(w, status, e)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ParseInput accepts a string or an array of strings (token-id arrays are
// refused: the server tokenizes itself).
func ParseInput(raw json.RawMessage) ([]string, error) {
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		if len(many) == 0 {
			return nil, fmt.Errorf("input is empty")
		}
		return many, nil
	}
	return nil, fmt.Errorf("input must be a string or an array of strings")
}

// RoleFor picks the role of a request (see the package comment above).
func RoleFor(req EmbeddingRequest, def string) (string, error) {
	switch strings.ToLower(req.InputType) {
	case "query", "search_query":
		return RoleQuery, nil
	case "passage", "search_document", "document":
		return RolePassage, nil
	case "":
	default:
		return "", fmt.Errorf("input_type %q: want query or passage", req.InputType)
	}
	switch {
	case strings.HasSuffix(req.Model, ":query"):
		return RoleQuery, nil
	case strings.HasSuffix(req.Model, ":passage"):
		return RolePassage, nil
	}
	return def, nil
}

func (s *EmbedServer) embeddings(w http.ResponseWriter, r *http.Request) {
	var req EmbeddingRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<20)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid_request_error", "body: "+err.Error())
		return
	}
	texts, err := ParseInput(req.Input)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	limit := s.MaxInputs
	if limit <= 0 {
		limit = 2048
	}
	if len(texts) > limit {
		fail(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("%d inputs, at most %d", len(texts), limit))
		return
	}
	if req.Dimensions != 0 && req.Dimensions != s.Emb.Dim() {
		fail(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("dimensions %d: this model has %d", req.Dimensions, s.Emb.Dim()))
		return
	}
	if req.EncodingFormat != "" && req.EncodingFormat != "float" && req.EncodingFormat != "base64" {
		fail(w, http.StatusBadRequest, "invalid_request_error", "encoding_format must be float or base64")
		return
	}
	role, err := RoleFor(req, s.role())
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	vecs, err := s.Emb.EmbedRole(r.Context(), role, texts)
	if err != nil {
		fail(w, http.StatusInternalServerError, "server_error", err.Error())
		return
	}
	resp := EmbeddingResponse{Object: "list", Model: s.Emb.ModelID(), Data: make([]EmbeddingData, len(vecs))}
	for i, v := range vecs {
		var e any = v
		if req.EncodingFormat == "base64" {
			e = base64.StdEncoding.EncodeToString(EncodeVec(v))
		}
		resp.Data[i] = EmbeddingData{Object: "embedding", Index: i, Embedding: e}
		resp.Usage.PromptTokens += Tokens(texts[i])
	}
	resp.Usage.TotalTokens = resp.Usage.PromptTokens
	s.mu.Lock()
	s.requests++
	s.texts += int64(len(texts))
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Client: the harness and the chanda server use a running embed server as
// their e5, so only one process on the machine holds the model.

// Remote is an embedder backed by an "lme embedserve" (or any server that
// returns the dim extension in /v1/models).
type Remote struct {
	base string
	id   string
	dim  int
	hc   *http.Client
}

// OpenRemote reads the model id and dimension from base/v1/models.
func OpenRemote(ctx context.Context, base string) (*Remote, error) {
	base = strings.TrimRight(base, "/")
	r := &Remote{base: base, hc: &http.Client{Timeout: 30 * time.Minute}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed server %s: %w", base, err)
	}
	defer resp.Body.Close()
	var ml struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ml); err != nil || len(ml.Data) != 1 || ml.Data[0].Dim <= 0 {
		return nil, fmt.Errorf("embed server %s: unexpected /v1/models answer (%v)", base, err)
	}
	r.id, r.dim = ml.Data[0].ID, ml.Data[0].Dim
	return r, nil
}

func (r *Remote) ModelID() string { return r.id }
func (r *Remote) Dim() int        { return r.dim }

func (r *Remote) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return r.call(ctx, RolePassage, texts)
}

func (r *Remote) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return r.call(ctx, RoleQuery, texts)
}

func (r *Remote) call(ctx context.Context, role string, texts []string) ([][]float32, error) {
	const batch = 256
	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += batch {
		part := texts[i:min(i+batch, len(texts))]
		in, _ := json.Marshal(part)
		body, _ := json.Marshal(EmbeddingRequest{Input: in, Model: r.id, EncodingFormat: "base64", InputType: role})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.base+"/v1/embeddings", bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := r.hc.Do(req)
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("embed server: %s: %s", resp.Status, bytes.TrimSpace(b))
		}
		var er struct {
			Model string `json:"model"`
			Data  []struct {
				Index     int    `json:"index"`
				Embedding string `json:"embedding"`
			} `json:"data"`
		}
		if err := json.Unmarshal(b, &er); err != nil {
			return nil, err
		}
		if er.Model != r.id || len(er.Data) != len(part) {
			return nil, fmt.Errorf("embed server: model %q with %d vectors, want %q with %d", er.Model, len(er.Data), r.id, len(part))
		}
		vs := make([][]float32, len(part))
		for _, d := range er.Data {
			raw, err := base64.StdEncoding.DecodeString(d.Embedding)
			if err != nil || d.Index < 0 || d.Index >= len(part) || len(raw) != 4*r.dim {
				return nil, fmt.Errorf("embed server: bad vector %d", d.Index)
			}
			vs[d.Index] = DecodeVec(raw)
		}
		out = append(out, vs...)
	}
	return out, nil
}
