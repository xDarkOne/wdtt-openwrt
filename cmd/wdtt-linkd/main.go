// Command wdtt-linkd is a tiny token-guarded HTTP endpoint for the WDTT server.
// It serves a handful of *fresh* VK call links to routers on demand, read live
// from the file the whitelist-bypass creator keeps rotating.
//
//	GET /<token>/links?n=4          -> n random current call links, text/plain
//	GET /<token>/links?n=4&slot=0   -> n *distinct* links reserved for this slot,
//	                                   so multiple routers get non-overlapping calls
//	                                   (slot 0,1,2… each own a contiguous window of
//	                                   the sorted pool; needs pool >= slots*n)
//	GET /<token>/health             -> "ok"
//
// Run it as a systemd service on the VPS (see deploy/wdtt-linkd.service).
package main

import (
	"bufio"
	"crypto/subtle"
	"flag"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	listen := flag.String("listen", "0.0.0.0:56090", "listen address")
	file := flag.String("file", "/opt/whitelist-bypass/active_call_vk.txt", "rotating call-link file")
	token := flag.String("token", "", "access token (required)")
	defN := flag.Int("n", 4, "default number of links to return")
	flag.Parse()

	if *token == "" {
		log.Fatal("wdtt-linkd: -token is required")
	}

	h := &handler{file: *file, token: *token, defN: *defN}
	srv := &http.Server{
		Addr:              *listen,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("wdtt-linkd: listening on %s, file=%s", *listen, *file)
	log.Fatal(srv.ListenAndServe())
}

type handler struct {
	file  string
	token string
	defN  int
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	// Constant-time token check to avoid timing oracles.
	if subtle.ConstantTimeCompare([]byte(parts[0]), []byte(h.token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	switch parts[1] {
	case "health":
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok\n"))
	case "links":
		h.serveLinks(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *handler) serveLinks(w http.ResponseWriter, r *http.Request) {
	n := h.defN
	if q := r.URL.Query().Get("n"); q != "" {
		if v, err := strconv.Atoi(q); err == nil && v > 0 && v <= 32 {
			n = v
		}
	}

	links, err := readLinks(h.file)
	if err != nil || len(links) == 0 {
		http.Error(w, "no links available", http.StatusServiceUnavailable)
		return
	}

	// If the router asks for a specific slot, hand it a deterministic,
	// non-overlapping window of the pool so multiple routers never share calls.
	// Otherwise fall back to the legacy random pick.
	if q := r.URL.Query().Get("slot"); q != "" {
		if slot, err := strconv.Atoi(q); err == nil && slot >= 0 {
			links = pickSlot(links, slot, n)
		} else {
			http.Error(w, "bad slot", http.StatusBadRequest)
			return
		}
	} else {
		rand.Shuffle(len(links), func(i, j int) { links[i], links[j] = links[j], links[i] })
		if len(links) > n {
			links = links[:n]
		}
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(strings.Join(links, "\n") + "\n"))
}

// pickSlot returns the n links that belong to the given slot. The pool is
// sorted so every router sees the same stable ordering, then partitioned into
// contiguous windows of n: slot 0 -> [0:n], slot 1 -> [n:2n], and so on. As
// long as len(links) >= (slot+1)*n the windows never overlap, so distinct
// routers get distinct calls. Indices wrap modulo the pool so an out-of-range
// slot degrades gracefully instead of returning nothing.
func pickSlot(links []string, slot, n int) []string {
	L := len(links)
	if L == 0 {
		return nil
	}
	if n > L {
		n = L
	}
	sort.Strings(links)
	start := (slot * n) % L
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, links[(start+i)%L])
	}
	return out
}

func readLinks(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []string
	seen := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.Contains(line, "/call/join/") {
			continue
		}
		if _, ok := seen[line]; ok {
			continue
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}
	return out, sc.Err()
}
