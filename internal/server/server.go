// Package server runs the local preview HTTP server with a built-in SSE
// live-reload endpoint at /__sitemaker/events.
package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	pathpkg "path"
	"strings"
	"sync"
	"time"
)

// LiveReloadSnippet is injected before </head> during `sitemaker serve`.
const LiveReloadSnippet = `<script>(function(){if(typeof EventSource==="undefined")return;var es=new EventSource("/__sitemaker/events");es.onmessage=function(e){if(e.data==="reload")location.reload();};})();</script>`

// Broker fans out reload notifications to connected browser tabs.
type Broker struct {
	mu      sync.Mutex
	clients map[chan struct{}]struct{}
}

// NewBroker constructs a live-reload broker.
func NewBroker() *Broker {
	return &Broker{clients: map[chan struct{}]struct{}{}}
}

// Reload tells every connected browser tab to refresh.
func (b *Broker) Reload() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (b *Broker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ch := make(chan struct{}, 1)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		delete(b.clients, ch)
		b.mu.Unlock()
	}()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			_, _ = fmt.Fprint(w, "data: reload\n\n")
			flusher.Flush()
		}
	}
}

// Options configures the preview server.
type Options struct {
	Host   string
	Port   int
	Dir    string
	Broker *Broker
}

// Serve serves Dir until ctx is cancelled or SIGINT arrives.
func Serve(ctx context.Context, opts Options) error {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Port == 0 {
		opts.Port = 8080
	}
	if opts.Dir == "" {
		opts.Dir = "public"
	}

	mux := http.NewServeMux()
	if opts.Broker != nil {
		mux.Handle("/__sitemaker/events", opts.Broker)
	}
	fs := http.FileServer(http.Dir(opts.Dir))
	mux.Handle("/", cacheMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &notFoundRecorder{ResponseWriter: w}
		fs.ServeHTTP(rec, r)
		if rec.status != http.StatusNotFound {
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if data, err := os.ReadFile(pathpkg.Join(opts.Dir, "404.html")); err == nil {
			_, _ = w.Write(data)
			return
		}
		_, _ = w.Write(rec.body.Bytes())
	})))

	addr := fmt.Sprintf("%s:%d", opts.Host, opts.Port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	go func() {
		<-shutdownCtx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()

	fmt.Printf("Serving %s at http://localhost:%d\n", opts.Dir, opts.Port)
	fmt.Println("Press Ctrl+C to stop")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func cacheMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", cacheControlForPath(r.URL.Path))
		next.ServeHTTP(w, r)
	})
}

func cacheControlForPath(requestPath string) string {
	cleanPath := pathpkg.Clean("/" + strings.TrimSpace(requestPath))
	ext := strings.ToLower(pathpkg.Ext(cleanPath))
	switch {
	case strings.HasPrefix(cleanPath, "/static/bundle.") && (ext == ".css" || ext == ".js"):
		return "public, max-age=31536000, immutable"
	case cleanPath == "/" || strings.HasSuffix(requestPath, "/") || ext == ".html" || ext == ".xml" || ext == ".txt" || ext == ".json":
		return "public, max-age=0, must-revalidate"
	default:
		return "public, max-age=2592000"
	}
}

type notFoundRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *notFoundRecorder) WriteHeader(code int) {
	r.status = code
	if code != http.StatusNotFound {
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *notFoundRecorder) Write(p []byte) (int, error) {
	if r.status == http.StatusNotFound {
		return r.body.Write(p)
	}
	return r.ResponseWriter.Write(p)
}
