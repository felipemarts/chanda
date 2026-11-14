// Command lmeproxy is the OpenAI-compatible reverse proxy that every LLM call
// of the LongMemEval battery goes through (reader, judge, Mem0 extraction,
// Chanda sleep phase 2).
//
// It forces temperature 0 (and, by default, DeepSeek's non-thinking mode, in
// which temperature is honoured), tags each call with a condition/role,
// records tokens and cost per call in an append-only JSONL file, keeps the
// running totals per tag and overall in a JSON file rewritten atomically, and
// refuses calls (HTTP 402) once the spend would cross -cap-usd.
//
// Tags: header X-Chanda-Eval-Tag, else the path prefix /tag/<name>/v1/...,
// else -default-tag. Clients that cannot set headers (Mem0, Chanda) use the
// path prefix as their base URL, e.g. http://127.0.0.1:8787/tag/C-ingest/v1.
//
// Run exactly one instance per cost file: the cap is enforced in process,
// and a lock file next to the cost file stops a second instance.
//
// With -store DIR, every successful upstream call is recorded in a
// content-addressed store keyed by the sha256 of the forwarded body (the
// request_sha256 of the log); with -replay as well, a request whose key is
// in the store is answered locally at no cost. -store-stats prints a summary
// of the store and exits.
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed prices.deepseek.json
var defaultPrices []byte

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lmeproxy:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("lmeproxy", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:8787", "listen address (keep it on loopback)")
	upstream := fs.String("upstream", "https://api.deepseek.com", "upstream OpenAI-compatible base URL (POST <upstream>/chat/completions)")
	keyEnv := fs.String("api-key-env", "DEEPSEEK_API_KEY", "environment variable holding the upstream API key")
	allowNoKey := fs.Bool("allow-no-key", false, "start even when the API key variable is empty (local fake upstreams only)")
	capUSD := fs.Float64("cap-usd", 0, "hard spending cap in USD across all tags (required)")
	pricesPath := fs.String("prices", "", "price table JSON (default: embedded prices.deepseek.json)")
	logPath := fs.String("log", "eval/llm_calls.jsonl", "append-only JSONL call log")
	costPath := fs.String("cost", "eval/cost.json", "running totals file (rewritten atomically)")
	defaultTag := fs.String("default-tag", "", "tag for requests that carry none (empty: such requests are rejected)")
	defaultMax := fs.Int("default-max-tokens", 4096, "max_tokens injected when a request sets none")
	thinking := fs.String("thinking", "disabled", "disabled: force DeepSeek non-thinking mode (temperature is ignored in thinking mode); passthrough: leave as sent")
	rejectSchema := fs.Bool("reject-json-schema", true, "answer response_format json_schema with a local 400 naming response_format (DeepSeek accepts only text/json_object)")
	offPeakOnly := fs.Bool("offpeak-only", false, "refuse calls during the price table's peak windows")
	peakGuard := fs.Duration("peak-guard", 15*time.Minute, "with -offpeak-only, also refuse calls when peak starts within this margin")
	window := fs.String("window", "", "optional allowed UTC window HH:MM-HH:MM (may cross midnight); calls outside are refused")
	timeout := fs.Duration("upstream-timeout", 15*time.Minute, "per-call upstream timeout")
	storeDir := fs.String("store", "", "record/replay store directory, e.g. eval/lme/llmstore (empty: off); successful calls are recorded whenever it is set")
	replay := fs.Bool("replay", false, "answer requests found in -store locally (cost 0, any hour, no cap reservation)")
	storeStats := fs.Bool("store-stats", false, "print statistics of -store and exit (no proxy is started)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*replay || *storeStats) && *storeDir == "" {
		return errors.New("-replay and -store-stats require -store")
	}
	if *storeStats {
		if _, err := os.Stat(*storeDir); err != nil {
			return err
		}
		st, err := (&Store{dir: *storeDir}).Stats()
		if err != nil {
			return err
		}
		st.WriteText(os.Stdout)
		return nil
	}
	var store *Store
	if *storeDir != "" {
		var err error
		if store, err = OpenStore(*storeDir); err != nil {
			return err
		}
	}
	if *capUSD <= 0 {
		return errors.New("-cap-usd is required and must be > 0")
	}
	raw := defaultPrices
	if *pricesPath != "" {
		b, err := os.ReadFile(*pricesPath)
		if err != nil {
			return err
		}
		raw = b
	}
	prices, err := ParsePriceTable(raw)
	if err != nil {
		return err
	}
	key := os.Getenv(*keyEnv)
	if key == "" && !*allowNoKey {
		return fmt.Errorf("%s is empty (use -allow-no-key only against a local fake upstream)", *keyEnv)
	}
	var dw *DailyWindow
	if *window != "" {
		if dw, err = ParseDailyWindow(*window); err != nil {
			return err
		}
	}
	unlock, err := lockFile(*costPath + ".lock")
	if err != nil {
		return err
	}
	defer unlock()

	p, err := NewProxy(Config{
		Upstream:         *upstream,
		APIKey:           key,
		CapUSD:           *capUSD,
		Prices:           prices,
		LogPath:          *logPath,
		CostPath:         *costPath,
		DefaultTag:       *defaultTag,
		DefaultMaxTokens: *defaultMax,
		Thinking:         *thinking,
		RejectJSONSchema: *rejectSchema,
		OffPeakOnly:      *offPeakOnly,
		PeakGuard:        *peakGuard,
		Window:           dw,
		Client:           &http.Client{Timeout: *timeout},
		Store:            store,
		Replay:           *replay,
	})
	if err != nil {
		return err
	}
	defer p.Close()

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	snap := p.Snapshot()
	fmt.Fprintf(os.Stderr, "lmeproxy: listening on %s -> %s; cap %.2f USD, already spent %.6f; log %s; totals %s\n",
		ln.Addr(), *upstream, *capUSD, snap.SpentUSD, *logPath, *costPath)
	if store != nil {
		fmt.Fprintf(os.Stderr, "lmeproxy: recording to %s; replay %v\n", *storeDir, *replay)
	}
	srv := &http.Server{Handler: p, ReadHeaderTimeout: 30 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-errc:
		return err
	case <-sig:
		fmt.Fprintln(os.Stderr, "lmeproxy: shutting down; waiting up to 2m for in-flight calls")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}
