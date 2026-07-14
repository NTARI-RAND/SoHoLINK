// Command sitesvc is the small public-site backend behind the frontend edge. It
// serves two marketing-surface needs the coordinator/portal should not carry:
//
//   - POST /api/waitlist — capture emails of prospective operators while
//     federation is not yet open (pending the Go 1.27 / ML-DSA-65 rollout).
//     Appended as JSONL to WAITLIST_FILE; deduplicated on write.
//   - GET  /api/news — the "communities vs. datacenters" live feed for the
//     landing page: real headlines fetched from a news RSS source on a timer,
//     cached in memory, served as JSON. No fabricated content.
//
// stdlib only. Bound on the compose network; the frontend proxies /api/waitlist
// and /api/news to it. It holds no secrets and touches no coordinator state.
package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// ---------------- waitlist ----------------

type waitlistEntry struct {
	Email string `json:"email"`
	Note  string `json:"note,omitempty"`
	At    string `json:"at"`
}

type waitlist struct {
	mu   sync.Mutex
	path string
}

// looksLikeEmail is a deliberately loose sanity check — enough to reject junk
// without pretending to validate deliverability (that is the confirm step's job
// when federation actually opens).
func looksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.IndexByte(s, '@')
	dot := strings.LastIndexByte(s, '.')
	return len(s) >= 6 && len(s) <= 254 && at > 0 && dot > at+1 && dot < len(s)-1 &&
		!strings.ContainsAny(s, " \t\r\n<>")
}

func (wl *waitlist) already(email string) bool {
	f, err := os.Open(wl.path)
	if err != nil {
		return false
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var e waitlistEntry
		if err := dec.Decode(&e); err != nil {
			break
		}
		if strings.EqualFold(e.Email, email) {
			return true
		}
	}
	return false
}

func (wl *waitlist) add(email, note, at string) error {
	wl.mu.Lock()
	defer wl.mu.Unlock()
	if wl.already(email) {
		return nil // idempotent: already on the list
	}
	f, err := os.OpenFile(wl.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(waitlistEntry{Email: email, Note: note, At: at}) //nolint:errcheck
	_, err = f.Write(append(b, '\n'))
	return err
}

func (wl *waitlist) handle(now func() time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Email string `json:"email"`
			Note  string `json:"note"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 8<<10)).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}
		email := strings.TrimSpace(req.Email)
		if !looksLikeEmail(email) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "please enter a valid email"})
			return
		}
		note := req.Note
		if len(note) > 500 {
			note = note[:500]
		}
		if err := wl.add(email, note, now().UTC().Format(time.RFC3339)); err != nil {
			slog.Error("waitlist write failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save; try again"})
			return
		}
		slog.Info("waitlist signup", "email_domain", email[strings.IndexByte(email, '@')+1:])
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ---------------- news feed ----------------

type newsItem struct {
	Title     string `json:"title"`
	Source    string `json:"source"`
	URL       string `json:"url"`
	Published string `json:"published,omitempty"`
}

type newsCache struct {
	mu      sync.RWMutex
	items   []newsItem
	updated time.Time
}

func (c *newsCache) get() ([]newsItem, time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items, c.updated
}

func (c *newsCache) set(items []newsItem, at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items, c.updated = items, at
}

// rss is the minimal shape of a news RSS feed (Google News search RSS).
type rss struct {
	Channel struct {
		Items []struct {
			Title   string `xml:"title"`
			Link    string `xml:"link"`
			PubDate string `xml:"pubDate"`
			Source  string `xml:"source"`
		} `xml:"item"`
	} `xml:"channel"`
}

func fetchNews(ctx context.Context, feedURL string, max int, now func() time.Time) ([]newsItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "soholink-sitesvc/1 (+https://soholink.org)")
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("news source returned " + resp.Status)
	}
	var doc rss
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	var out []newsItem
	for _, it := range doc.Channel.Items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		// Google News titles are "Headline - Source"; split the source out.
		src := strings.TrimSpace(it.Source)
		if src == "" {
			if i := strings.LastIndex(title, " - "); i > 0 {
				src = strings.TrimSpace(title[i+3:])
				title = strings.TrimSpace(title[:i])
			}
		}
		out = append(out, newsItem{Title: title, Source: src, URL: strings.TrimSpace(it.Link), Published: strings.TrimSpace(it.PubDate)})
		if len(out) >= max {
			break
		}
	}
	return out, nil
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	addr := env("SITESVC_ADDR", ":8095")
	wl := &waitlist{path: env("WAITLIST_FILE", "/data/waitlist.jsonl")}
	feedURL := env("NEWS_FEED_URL", "https://news.google.com/rss/search?"+url.Values{
		"q":    {`"data center" (opposition OR moratorium OR "water use" OR residents OR zoning)`},
		"hl":   {"en-US"},
		"gl":   {"US"},
		"ceid": {"US:en"},
	}.Encode())
	refresh := 6 * time.Hour
	if d, err := time.ParseDuration(env("NEWS_REFRESH", "")); err == nil && d > 0 {
		refresh = d
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	nc := &newsCache{}
	// Fetch loop: on boot, then on the refresh timer. Fail-open — a fetch error
	// leaves the previous cache (or empty, and the frontend falls back to its
	// static dispatches).
	go func() {
		t := time.NewTicker(refresh)
		defer t.Stop()
		for {
			fctx, cancel := context.WithTimeout(ctx, 25*time.Second)
			items, err := fetchNews(fctx, feedURL, 8, time.Now)
			cancel()
			if err != nil {
				slog.Warn("news fetch failed; keeping previous cache", "error", err)
			} else {
				nc.set(items, time.Now().UTC())
				slog.Info("news refreshed", "items", len(items))
			}
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/waitlist", wl.handle(time.Now))
	mux.HandleFunc("GET /api/news", func(w http.ResponseWriter, r *http.Request) {
		items, updated := nc.get()
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "updated": updated})
	})
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]string{"status": "ok"}) })

	srv := &http.Server{Addr: addr, Handler: mux, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second}
	go func() {
		slog.Info("sitesvc listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("sitesvc error", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx) //nolint:errcheck
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v) //nolint:errcheck
}
