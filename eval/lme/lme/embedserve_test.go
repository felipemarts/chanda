package lme

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
)

func hashServer(t *testing.T, role string) (*httptest.Server, *Cached) {
	t.Helper()
	emb, err := OpenEmbedder(t.Context(), EmbedOptions{Kind: "hash"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer((&EmbedServer{Emb: emb, DefaultRole: role}).Handler())
	t.Cleanup(srv.Close)
	return srv, emb
}

func post(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url+"/v1/embeddings", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, m
}

func TestEmbedServerOpenAIShape(t *testing.T) {
	srv, emb := hashServer(t, RoleQuery)
	code, m := post(t, srv.URL, `{"model":"anything","input":["The cat sleeps.","Dogs bark."]}`)
	if code != 200 || m["object"] != "list" || m["model"] != "hash-v1-256" {
		t.Fatalf("%d %v", code, m)
	}
	data := m["data"].([]any)
	if len(data) != 2 {
		t.Fatalf("data %v", data)
	}
	d0 := data[0].(map[string]any)
	if d0["object"] != "embedding" || d0["index"].(float64) != 0 || len(d0["embedding"].([]any)) != 256 {
		t.Fatalf("item %v", d0)
	}
	usage := m["usage"].(map[string]any)
	if usage["prompt_tokens"].(float64) != float64(Tokens("The cat sleeps.")+Tokens("Dogs bark.")) || usage["total_tokens"] != usage["prompt_tokens"] {
		t.Fatalf("usage %v", usage)
	}
	// The vector is exactly the embedder's (float32 survives JSON).
	want, _ := emb.Embed(t.Context(), []string{"The cat sleeps."})
	for i, x := range d0["embedding"].([]any) {
		if float32(x.(float64)) != want[0][i] {
			t.Fatalf("component %d differs", i)
		}
	}
	// A single string input and base64 encoding.
	code, m = post(t, srv.URL, `{"model":"m","input":"The cat sleeps.","encoding_format":"base64"}`)
	if code != 200 {
		t.Fatalf("%d %v", code, m)
	}
	raw, err := base64.StdEncoding.DecodeString(m["data"].([]any)[0].(map[string]any)["embedding"].(string))
	if err != nil || !reflect.DeepEqual(DecodeVec(raw), want[0]) {
		t.Fatalf("base64 vector differs (%v)", err)
	}
	// /v1/models names the model with its dimension.
	resp, err := http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	var ml struct{ Data []ModelInfo }
	_ = json.NewDecoder(resp.Body).Decode(&ml)
	resp.Body.Close()
	if len(ml.Data) != 1 || ml.Data[0].ID != "hash-v1-256" || ml.Data[0].Dim != 256 || ml.Data[0].DefaultRole != RoleQuery {
		t.Fatalf("models %+v", ml)
	}
}

func TestEmbedServerErrors(t *testing.T) {
	srv, _ := hashServer(t, RoleQuery)
	for _, body := range []string{
		`{"model":"m","input":[1,2,3]}`,
		`{"model":"m","input":[]}`,
		`{"model":"m","input":"x","dimensions":3}`,
		`{"model":"m","input":"x","encoding_format":"int8"}`,
		`{"model":"m","input":"x","input_type":"classification"}`,
		`not json`,
	} {
		code, m := post(t, srv.URL, body)
		e, _ := m["error"].(map[string]any)
		if code != 400 || e == nil || e["message"] == "" || e["type"] != "invalid_request_error" {
			t.Fatalf("%s: %d %v", body, code, m)
		}
	}
}

func TestRoleFor(t *testing.T) {
	cases := []struct {
		req  EmbeddingRequest
		def  string
		want string
	}{
		{EmbeddingRequest{Model: "e5"}, RoleQuery, RoleQuery},
		{EmbeddingRequest{Model: "e5"}, RolePassage, RolePassage},
		{EmbeddingRequest{Model: "e5:passage"}, RoleQuery, RolePassage},
		{EmbeddingRequest{Model: "e5:query"}, RolePassage, RoleQuery},
		{EmbeddingRequest{Model: "e5:query", InputType: "search_document"}, RoleQuery, RolePassage},
		{EmbeddingRequest{InputType: "query"}, RolePassage, RoleQuery},
	}
	for _, c := range cases {
		if got, err := RoleFor(c.req, c.def); err != nil || got != c.want {
			t.Fatalf("%+v: %q %v", c, got, err)
		}
	}
}

// The remote client, as the harness and the chanda server use it, returns
// bit-identical vectors and the wrapped model id, through the cache.
func TestRemoteAndCacheRoundTrip(t *testing.T) {
	srv, local := hashServer(t, RoleQuery)
	cache := filepath.Join(t.TempDir(), "c.sqlite")
	r, err := OpenEmbedder(t.Context(), EmbedOptions{Kind: "hash", URL: srv.URL, Cache: cache})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.ModelID() != local.ModelID() || r.Dim() != 256 {
		t.Fatalf("%s %d", r.ModelID(), r.Dim())
	}
	texts := []string{"alpha beta", "gamma", "alpha beta"}
	for round := 0; round < 2; round++ {
		got, err := r.Embed(t.Context(), texts)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := local.Embed(t.Context(), texts)
		if !reflect.DeepEqual(got, want) {
			t.Fatal("remote vectors differ")
		}
		q, _ := r.EmbedQuery(t.Context(), texts[:1])
		if !reflect.DeepEqual(q[0], want[0]) {
			t.Fatal("hash query vector differs from passage vector")
		}
	}
	hits, misses := r.Stats()
	// Round 1: 3 passage misses + 1 query miss; round 2: all hits.
	if misses != 4 || hits != 4 {
		t.Fatalf("hits %d misses %d", hits, misses)
	}
	if _, err := OpenEmbedder(t.Context(), EmbedOptions{Kind: "e5", URL: srv.URL}); err == nil {
		t.Fatal("hash server accepted as e5")
	}
}
