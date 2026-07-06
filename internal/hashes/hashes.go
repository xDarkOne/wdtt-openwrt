// Package hashes fetches and normalizes the VK "call join" tokens the client
// uses to obtain TURN relay credentials. The server keeps a rotating list of
// live calls (active_call_vk.txt); we pull it over HTTP (or read a local file)
// and reduce each entry to its bare token.
package hashes

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

const joinPrefix = "vk.com/call/join/"

// Source describes where to read call links from. Exactly one of URL / File
// is used (URL takes precedence).
type Source struct {
	URL  string
	File string
}

// Fetch returns a deduplicated, sorted list of bare call tokens.
func Fetch(ctx context.Context, src Source) ([]string, error) {
	var raw string
	var err error
	switch {
	case src.URL != "":
		raw, err = fetchURL(ctx, src.URL)
	case src.File != "":
		raw, err = readFile(src.File)
	default:
		return nil, fmt.Errorf("no hash source configured")
	}
	if err != nil {
		return nil, err
	}
	tokens := parse(raw)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("hash source returned no usable call tokens")
	}
	return tokens, nil
}

func fetchURL(ctx context.Context, url string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "wdtt-openwrt/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("hash source %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// parse extracts bare tokens from a blob that may contain full call URLs,
// bare tokens, blank lines and comments.
func parse(raw string) []string {
	seen := make(map[string]struct{})
	var out []string
	sc := bufio.NewScanner(strings.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tok := normalize(line)
		if tok == "" {
			continue
		}
		if _, ok := seen[tok]; ok {
			continue
		}
		seen[tok] = struct{}{}
		out = append(out, tok)
	}
	sort.Strings(out)
	return out
}

// normalize reduces a line to the bare token after ".../call/join/".
func normalize(line string) string {
	if i := strings.Index(line, joinPrefix); i >= 0 {
		line = line[i+len(joinPrefix):]
	}
	// Strip any trailing query/fragment or whitespace.
	line = strings.TrimSpace(line)
	if j := strings.IndexAny(line, "?#/ \t"); j >= 0 {
		line = line[:j]
	}
	return line
}

// Equal reports whether two token lists are identical (order-independent since
// Fetch returns sorted output).
func Equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
