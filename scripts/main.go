package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultPort       = "3000"
	defaultOnionLand  = "https://www.onionland.to/search?q=%s&page=%d"
	defaultTorDex     = "http://tordexu73joywapk2txdr54jed4imqledpcvcuf75qsas2gwdgksvnyd.onion/search?query=%s&page=%d"
	defaultTorSocks   = "127.0.0.1:9050"
	defaultDuckHTML   = "https://html.duckduckgo.com/html/?q=%s&s=%d"
	defaultDuckLite   = "https://lite.duckduckgo.com/lite/?q=%s"
	defaultBingSearch = "https://www.bing.com/search?q=%s&first=%d&count=30"
	defaultPageSize   = 15
	maxSearchPages    = 3
	maxProviderBytes  = 4 << 20
	providerTimeout   = 20 * time.Second
	cacheTTL          = 2 * time.Minute
	maxQueryLength    = 120
)

type result struct {
	Title   string
	URL     string
	Snippet string
	Score   int
}

type imageResult struct {
	Title     string
	ImageURL  string
	SourceURL string
	Thumbnail string
}

type cachedSearch struct {
	Created time.Time
	Results []result
}

type searchCache struct {
	mu      sync.RWMutex
	entries map[string]cachedSearch
}

var cache = searchCache{entries: make(map[string]cachedSearch)}

var (
	onionAnchorPattern = regexp.MustCompile(`(?is)<a\b[^>]*href\s*=\s*["']([^"']+)[^>]*>(.*?)</a>`)
	onionURLPattern    = regexp.MustCompile(`(?i)https?://(?:[a-z2-7]{16}|[a-z2-7]{56})\.onion(?:\:\d+)?(?:/[^\s"'<>)]*)?`)
	tagPattern         = regexp.MustCompile(`(?is)<[^>]+>`)
	spacePattern       = regexp.MustCompile(`\s+`)
	tokenPattern       = regexp.MustCompile(`[\p{L}\p{N}][\p{L}\p{N}._-]{1,63}`)
	ddgAnchorPattern   = regexp.MustCompile(`(?is)<a[^>]+class=["'][^"']*result__a[^"']*["'][^>]+href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	ddgSnippetPattern  = regexp.MustCompile(`(?is)<[^>]+class=["'][^"']*result__snippet[^"']*["'][^>]*>(.*?)</[^>]+>`)
	bingIuscPattern    = regexp.MustCompile(`(?is)<a[^>]+class=["'][^"']*iusc[^"']*["'][^>]*\bm\s*=\s*(?:'([^']*)'|"([^"]*)")[^>]*>`)
	bingResultPattern  = regexp.MustCompile(`(?is)<li\b[^>]*class=["'][^"']*\bb_algo\b[^"']*["'][^>]*>(.*?)</li>`)
	bingResultLink     = regexp.MustCompile(`(?is)<h2\b[^>]*>\s*<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	bingResultSnippet  = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
)

func main() {
	http.HandleFunc("/", home)
	http.HandleFunc("/search", search)
	http.HandleFunc("/health", health)
	http.HandleFunc("/download", downloadImage)

	bind := os.Getenv("LIEN_BIND")
	if bind == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = defaultPort
		}
		bind = "127.0.0.1:" + port
	}
	server := &http.Server{Addr: bind, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	log.Printf("Lien search engine listening on http://%s", bind)
	log.Fatal(server.ListenAndServe())
}

func home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := os.ReadFile("index.html")
	if err != nil {
		http.Error(w, "Lien is unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

func health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func search(w http.ResponseWriter, r *http.Request) {
	query := normalizeQuery(r.URL.Query().Get("q"))
	if query == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if len(query) > maxQueryLength {
		http.Error(w, "Search query is too long", http.StatusBadRequest)
		return
	}

	searchType := r.URL.Query().Get("type")
	if searchType != "onion" {
		searchType = "clear"
	}
	tab := r.URL.Query().Get("tab")
	if tab != "images" {
		tab = "all"
	}
	page := parsePage(r.URL.Query().Get("page"))

	if tab == "images" {
		images, err := searchImages(r.Context(), query, searchType)
		if err != nil {
			writeSearchUnavailable(w, query, searchType, "images")
			return
		}
		writeImageResults(w, query, searchType, images, page)
		return
	}

	results, err := searchIndex(r.Context(), query, searchType)
	if err != nil {
		writeSearchUnavailable(w, query, searchType, "all")
		return
	}
	start := (page - 1) * defaultPageSize
	if start > len(results) {
		start = len(results)
	}
	end := start + defaultPageSize
	if end > len(results) {
		end = len(results)
	}
	writeResults(w, query, searchType, results[start:end], page, len(results))
}

func parsePage(value string) int {
	page, err := strconv.Atoi(value)
	if err != nil || page < 1 {
		return 1
	}
	if page > 20 {
		return 20
	}
	return page
}

func normalizeQuery(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func searchIndex(ctx context.Context, query, searchType string) ([]result, error) {
	key := searchType + "|" + strings.ToLower(query)
	if results, ok := cacheGet(key); ok {
		return results, nil
	}

	var (
		results []result
		err     error
	)
	if searchType == "onion" {
		results, err = searchOnion(ctx, query)
	} else {
		results, err = searchClear(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	results = rankResults(query, results)
	cachePut(key, results)
	return results, nil
}

func searchClear(ctx context.Context, query string) ([]result, error) {
	type provider struct {
		name  string
		fetch func(context.Context, string, int) ([]result, error)
	}
	providers := []provider{
		{"DuckDuckGo HTML", func(ctx context.Context, q string, page int) ([]result, error) {
			return fetchDuckDuckGoPage(ctx, q, page)
		}},
		{"DuckDuckGo Lite", func(ctx context.Context, q string, page int) ([]result, error) {
			return fetchDuckDuckGoLitePage(ctx, q, page)
		}},
		{"Bing", func(ctx context.Context, q string, page int) ([]result, error) {
			return fetchBingSearchPage(ctx, q, page)
		}},
	}

	out := make(chan []result, len(providers))
	errs := make(chan error, len(providers))
	var wg sync.WaitGroup
	for _, p := range providers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			var all []result
			var lastErr error
			for page := 1; page <= maxSearchPages; page++ {
				res, err := p.fetch(ctx, query, page)
				if err != nil {
					lastErr = err
					continue
				}
				all = append(all, res...)
			}
			if len(all) > 0 {
				out <- all
				return
			}
			if lastErr == nil {
				lastErr = fmt.Errorf("no results")
			}
			errs <- fmt.Errorf("%s: %w", p.name, lastErr)
		}()
	}
	wg.Wait()
	close(out)
	close(errs)

	merged := make(map[string]result)
	for items := range out {
		for _, item := range items {
			if !isClearResultURL(item.URL) {
				continue
			}
			key := canonicalResultKey(item.URL)
			if key == "" {
				continue
			}
			if existing, ok := merged[key]; ok {
				if len(item.Snippet) > len(existing.Snippet) {
					existing.Snippet = item.Snippet
				}
				if len(item.Title) > len(existing.Title) {
					existing.Title = item.Title
				}
				merged[key] = existing
			} else {
				merged[key] = item
			}
		}
	}
	if len(merged) == 0 {
		var messages []string
		for err := range errs {
			messages = append(messages, err.Error())
		}
		if len(messages) > 0 {
			return nil, fmt.Errorf("clear-web providers unavailable: %s", strings.Join(messages, "; "))
		}
		return nil, fmt.Errorf("no clear-web results")
	}

	results := make([]result, 0, len(merged))
	for _, item := range merged {
		results = append(results, item)
	}
	return results, nil
}

func clearWebClients() []*http.Client {
	timeout := providerTimeout
	if value, ok := os.LookupEnv("LIEN_CLEAR_TIMEOUT"); ok {
		if seconds, err := strconv.Atoi(value); err == nil && seconds >= 5 && seconds <= 60 {
			timeout = time.Duration(seconds) * time.Second
		}
	}

	clients := make([]*http.Client, 0, 2)
	viaTor := os.Getenv("LIEN_CLEAR_VIA_TOR")
	if viaTor == "" {
		viaTor = "1"
	}
	if viaTor == "1" {
		socksAddr := os.Getenv("LIEN_CLEAR_TOR_SOCKS")
		if socksAddr == "" {
			socksAddr = os.Getenv("LIEN_TOR_SOCKS")
		}
		if socksAddr == "" {
			socksAddr = defaultTorSocks
		}

		transport := &http.Transport{
			Proxy:                 nil,
			DialContext:           (&socks5Dialer{address: socksAddr}).DialContext,
			DisableKeepAlives:     true,
			ForceAttemptHTTP2:     false,
			ResponseHeaderTimeout: timeout,
		}
		clients = append(clients, &http.Client{Transport: transport, Timeout: timeout})
	}
	clients = append(clients, &http.Client{Timeout: timeout})
	return clients
}

func fetchDuckDuckGoPage(ctx context.Context, query string, page int) ([]result, error) {
	endpoint := os.Getenv("LIEN_DDG_URL")
	if endpoint == "" {
		endpoint = defaultDuckHTML
	}
	start := (page - 1) * 30
	searchURL, err := fmtEndpoint(endpoint, query, start)
	if err != nil {
		return nil, err
	}
	body, err := fetchSearchHTML(ctx, searchURL, "DuckDuckGo")
	if err != nil {
		return nil, err
	}
	results := parseDuckDuckGoHTML(body)
	if len(results) == 0 {
		return nil, fmt.Errorf("DuckDuckGo returned no parseable results")
	}
	return results, nil
}

func fetchDuckDuckGoLitePage(ctx context.Context, query string, page int) ([]result, error) {
	endpoint := os.Getenv("LIEN_DDG_LITE_URL")
	if endpoint == "" {
		endpoint = defaultDuckLite
	}
	searchURL, err := fmtEndpoint(endpoint, query, page)
	if err != nil {
		return nil, err
	}
	body, err := fetchSearchHTML(ctx, searchURL, "DuckDuckGo Lite")
	if err != nil {
		return nil, err
	}
	results := parseDuckDuckGoLiteHTML(body)
	if len(results) == 0 {
		return nil, fmt.Errorf("DuckDuckGo Lite returned no parseable results")
	}
	return results, nil
}

func fetchBingSearchPage(ctx context.Context, query string, page int) ([]result, error) {
	endpoint := os.Getenv("LIEN_BING_SEARCH_URL")
	if endpoint == "" {
		endpoint = defaultBingSearch
	}
	first := (page-1)*30 + 1
	searchURL, err := fmtEndpoint(endpoint, query, first)
	if err != nil {
		return nil, err
	}
	body, err := fetchSearchHTML(ctx, searchURL, "Bing")
	if err != nil {
		return nil, err
	}
	results := parseBingSearchHTML(body)
	if len(results) == 0 {
		return nil, fmt.Errorf("Bing returned no parseable results")
	}
	return results, nil
}

func fmtEndpoint(endpoint, query string, pageValue int) (string, error) {
	encoded := url.QueryEscape(query)
	if strings.Contains(endpoint, "%d") {
		return fmt.Sprintf(endpoint, encoded, pageValue), nil
	}
	if strings.Contains(endpoint, "%s") {
		return fmt.Sprintf(endpoint, encoded), nil
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	values := parsed.Query()
	values.Set("q", query)
	parsed.RawQuery = values.Encode()
	return parsed.String(), nil
}

func fetchSearchHTML(ctx context.Context, searchURL, provider string) ([]byte, error) {
	var lastErr error
	for _, client := range clearWebClients() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Android) AppleWebKit/537.36 Chrome/124 Safari/537.36 Lien/2.3")
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%s request failed: %w", provider, err)
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProviderBytes))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			lastErr = fmt.Errorf("%s returned HTTP %d", provider, resp.StatusCode)
			continue
		}
		return body, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("%s request failed", provider)
	}
	return nil, lastErr
}

func parseDuckDuckGoHTML(body []byte) []result {
	matches := ddgAnchorPattern.FindAllSubmatch(body, -1)
	results := make([]result, 0, len(matches))
	seen := map[string]bool{}
	snippets := ddgSnippetPattern.FindAllSubmatch(body, -1)
	for i, m := range matches {
		if len(m) < 3 {
			continue
		}
		rawURL := html.UnescapeString(string(m[1]))
		if strings.HasPrefix(rawURL, "//duckduckgo.com/l/") {
			if parsed, err := url.Parse("https:" + rawURL); err == nil {
				rawURL = parsed.Query().Get("uddg")
			}
		}
		if u, err := url.QueryUnescape(rawURL); err == nil {
			rawURL = u
		}
		u, err := url.Parse(rawURL)
		if err != nil || !isClearResultURL(u.String()) {
			continue
		}
		u.Fragment = ""
		finalURL := u.String()
		key := canonicalResultKey(finalURL)
		if seen[key] {
			continue
		}
		seen[key] = true
		title := cleanText(string(m[2]))
		snippet := ""
		if i < len(snippets) {
			snippet = cleanText(string(snippets[i][1]))
		}
		results = append(results, result{Title: title, URL: finalURL, Snippet: snippet})
	}
	return results
}

func parseDuckDuckGoLiteHTML(body []byte) []result {
	anchorPattern := regexp.MustCompile(`(?is)<a\b[^>]*href=["']([^"']+)["'][^>]*>(.*?)</a>`)
	results := make([]result, 0)
	seen := map[string]bool{}
	for _, m := range anchorPattern.FindAllSubmatch(body, -1) {
		if len(m) < 3 {
			continue
		}
		rawURL := html.UnescapeString(string(m[1]))
		if strings.HasPrefix(rawURL, "//duckduckgo.com/l/") {
			if parsed, err := url.Parse("https:" + rawURL); err == nil {
				rawURL = parsed.Query().Get("uddg")
			}
		}
		if u, err := url.QueryUnescape(rawURL); err == nil {
			rawURL = u
		}
		u, err := url.Parse(rawURL)
		if err != nil || !isClearResultURL(u.String()) {
			continue
		}
		u.Fragment = ""
		finalURL := u.String()
		key := canonicalResultKey(finalURL)
		if seen[key] {
			continue
		}
		seen[key] = true
		title := cleanText(string(m[2]))
		if title == "" || strings.EqualFold(title, "More") {
			continue
		}
		results = append(results, result{Title: title, URL: finalURL})
	}
	return results
}

func parseBingSearchHTML(body []byte) []result {
	blocks := bingResultPattern.FindAllSubmatch(body, -1)
	results := make([]result, 0, len(blocks))
	seen := map[string]bool{}
	for _, block := range blocks {
		if len(block) < 2 {
			continue
		}
		link := bingResultLink.FindSubmatch(block[1])
		if len(link) < 3 {
			continue
		}
		u, err := url.Parse(html.UnescapeString(string(link[1])))
		if err != nil || !isClearResultURL(u.String()) {
			continue
		}
		u.Fragment = ""
		finalURL := u.String()
		key := canonicalResultKey(finalURL)
		if seen[key] {
			continue
		}
		seen[key] = true
		snippet := ""
		if p := bingResultSnippet.FindSubmatch(block[1]); len(p) >= 2 {
			snippet = cleanText(string(p[1]))
		}
		results = append(results, result{Title: cleanText(string(link[2])), URL: finalURL, Snippet: snippet})
	}
	return results
}

func fetchDuckDuckGo(ctx context.Context, query string) ([]result, error) {
	var all []result
	for page := 1; page <= maxSearchPages; page++ {
		results, err := fetchDuckDuckGoPage(ctx, query, page)
		if err != nil {
			return nil, err
		}
		all = append(all, results...)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("DuckDuckGo returned no results")
	}
	return dedupeResults(all), nil
}

func searchOnion(ctx context.Context, query string) ([]result, error) {
	onionLandTemplate := os.Getenv("LIEN_ONIONLAND_URL")
	if onionLandTemplate == "" {
		onionLandTemplate = defaultOnionLand
	}
	torDexTemplate := os.Getenv("LIEN_TORDEX_URL")
	if torDexTemplate == "" {
		torDexTemplate = defaultTorDex
	}
	torSocks := os.Getenv("LIEN_TOR_SOCKS")
	if torSocks == "" {
		torSocks = defaultTorSocks
	}

	type provider struct {
		name, tmpl string
		useTor     bool
	}
	providers := []provider{{"TorDex", torDexTemplate, true}, {"OnionLand", onionLandTemplate, false}}
	out := make(chan []result, len(providers))
	errCh := make(chan error, len(providers))
	var wg sync.WaitGroup
	for _, p := range providers {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			var all []result
			for page := 1; page <= maxSearchPages; page++ {
				var res []result
				var err error
				if p.useTor {
					res, err = fetchTorProviderPage(ctx, p.tmpl, query, page, torSocks)
				} else {
					res, err = fetchHTTPProviderPage(ctx, p.tmpl, query, page)
				}
				if err != nil {
					errCh <- fmt.Errorf("%s: %w", p.name, err)
					return
				}
				all = append(all, res...)
			}
			out <- all
		}()
	}
	wg.Wait()
	close(out)
	close(errCh)

	merged := make(map[string]result)
	for items := range out {
		for _, item := range items {
			key := canonicalResultKey(item.URL)
			if key == "" {
				continue
			}
			if existing, ok := merged[key]; ok {
				if len(item.Snippet) > len(existing.Snippet) {
					existing.Snippet = item.Snippet
				}
				if len(item.Title) > len(existing.Title) {
					existing.Title = item.Title
				}
				merged[key] = existing
			} else {
				merged[key] = item
			}
		}
	}
	if len(merged) == 0 {
		var errs []string
		for err := range errCh {
			errs = append(errs, err.Error())
		}
		if len(errs) > 0 {
			return nil, fmt.Errorf("all onion search providers failed: %s", strings.Join(errs, "; "))
		}
		return nil, fmt.Errorf("no onion results")
	}
	results := make([]result, 0, len(merged))
	for _, item := range merged {
		results = append(results, item)
	}
	return results, nil
}

func searchImages(ctx context.Context, query, searchType string) ([]imageResult, error) {
	if searchType == "onion" {
		return searchOnionImages(ctx, query)
	}
	return fetchBingImages(ctx, query)
}

func fetchBingImages(ctx context.Context, query string) ([]imageResult, error) {
	u := "https://www.bing.com/images/search?q=" + url.QueryEscape(query) + "&form=HDRSC2"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Android) AppleWebKit/537.36 Chrome/124 Safari/537.36 Lien/2.3")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := (&http.Client{Timeout: providerTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxProviderBytes))
	resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Bing Images returned HTTP %d", resp.StatusCode)
	}
	return parseBingImages(body), nil
}

func parseBingImages(body []byte) []imageResult {
	matches := bingIuscPattern.FindAllSubmatch(body, -1)
	out := make([]imageResult, 0, len(matches))
	seen := map[string]bool{}
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		rawMeta := string(m[1])
		if rawMeta == "" {
			rawMeta = string(m[2])
		}
		var data map[string]any
		if err := json.Unmarshal([]byte(html.UnescapeString(rawMeta)), &data); err != nil {
			continue
		}
		img, _ := data["murl"].(string)
		thumb, _ := data["turl"].(string)
		source, _ := data["purl"].(string)
		title, _ := data["t"].(string)
		if img == "" {
			continue
		}
		if seen[img] {
			continue
		}
		seen[img] = true
		out = append(out, imageResult{Title: cleanText(title), ImageURL: img, SourceURL: source, Thumbnail: thumb})
		if len(out) >= 60 {
			break
		}
	}
	if len(out) == 0 {
		return parseBingImagesFallback(body)
	}
	return out
}

func parseBingImagesFallback(body []byte) []imageResult {
	return nil
}

func searchOnionImages(ctx context.Context, query string) ([]imageResult, error) {
	tmpl := os.Getenv("LIEN_ONIONLAND_URL")
	if tmpl == "" {
		tmpl = defaultOnionLand
	}
	bodyResults := make([]imageResult, 0)
	for page := 1; page <= 2; page++ {
		u := fmt.Sprintf(tmpl, url.QueryEscape(query), page)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Lien/2.3")
		req.Header.Set("Accept", "text/html,application/xhtml+xml")
		resp, err := (&http.Client{Timeout: providerTimeout}).Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxProviderBytes))
		resp.Body.Close()
		bodyResults = append(bodyResults, extractOnionImages(body)...)
	}
	if len(bodyResults) == 0 {
		return nil, fmt.Errorf("no onion images found")
	}
	return dedupeImages(bodyResults), nil
}

func extractOnionImages(body []byte) []imageResult {
	pat := regexp.MustCompile(`(?is)<img\b[^>]*(?:src|data-src)=["']([^"']+)["'][^>]*>`)
	matches := pat.FindAllSubmatch(body, -1)
	var out []imageResult
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		raw := html.UnescapeString(string(m[1]))
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Scheme == "" || u.Hostname() == "" {
			continue
		}
		out = append(out, imageResult{Title: "Onion image", ImageURL: u.String(), SourceURL: u.String(), Thumbnail: u.String()})
		if len(out) >= 60 {
			break
		}
	}
	return out
}

func dedupeResults(results []result) []result {
	seen := make(map[string]bool)
	out := make([]result, 0, len(results))
	for _, item := range results {
		key := canonicalResultKey(item.URL)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func dedupeImages(results []imageResult) []imageResult {
	seen := map[string]bool{}
	out := make([]imageResult, 0, len(results))
	for _, item := range results {
		if item.ImageURL == "" || seen[item.ImageURL] {
			continue
		}
		seen[item.ImageURL] = true
		out = append(out, item)
	}
	return out
}

func fetchHTTPProviderPage(ctx context.Context, template, query string, page int) ([]result, error) {
	searchURL := fmt.Sprintf(template, url.QueryEscape(query), page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Lien/2.3")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	resp, err := (&http.Client{Timeout: providerTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderBytes))
	if err != nil {
		return nil, err
	}
	return parseProviderHTML(body), nil
}

func fetchTorProviderPage(ctx context.Context, template, query string, page int, socksAddr string) ([]result, error) {
	searchURL := fmt.Sprintf(template, url.QueryEscape(query), page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Lien/2.3")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	dialer := &socks5Dialer{address: socksAddr}
	transport := &http.Transport{Proxy: nil, DialContext: dialer.DialContext, DisableKeepAlives: true, ForceAttemptHTTP2: false, ResponseHeaderTimeout: providerTimeout}
	client := &http.Client{Transport: transport, Timeout: providerTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("TorDex returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProviderBytes))
	if err != nil {
		return nil, err
	}
	return parseProviderHTML(body), nil
}

type socks5Dialer struct{ address string }

func (d *socks5Dialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var nd net.Dialer
	conn, err := nd.DialContext(ctx, "tcp", d.address)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(providerTimeout)); err != nil {
		conn.Close()
		return nil, err
	}
	if err := socks5Handshake(conn); err != nil {
		conn.Close()
		return nil, err
	}
	if err := socks5Connect(conn, address); err != nil {
		conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

func socks5Handshake(conn net.Conn) error {
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	r := make([]byte, 2)
	if _, err := io.ReadFull(conn, r); err != nil {
		return err
	}
	if r[0] != 0x05 || r[1] != 0x00 {
		return fmt.Errorf("SOCKS5 proxy requires unsupported authentication")
	}
	return nil
}

func socks5Connect(conn net.Conn, address string) error {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid destination port")
	}
	req := []byte{0x05, 0x01, 0x00}
	if ip := net.ParseIP(host); ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			req = append(req, 0x01)
			req = append(req, ip4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return fmt.Errorf("destination hostname too long")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, []byte(host)...)
	}
	var pb [2]byte
	binary.BigEndian.PutUint16(pb[:], uint16(port))
	req = append(req, pb[:]...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	r := make([]byte, 4)
	if _, err := io.ReadFull(conn, r); err != nil {
		return err
	}
	if r[0] != 0x05 || r[1] != 0x00 {
		return fmt.Errorf("SOCKS5 connect failed with code 0x%02x", r[1])
	}
	skip := 0
	switch r[3] {
	case 0x01:
		skip = 4
	case 0x04:
		skip = 16
	case 0x03:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return err
		}
		skip = int(l[0])
	default:
		return fmt.Errorf("unknown SOCKS5 address type")
	}
	_, err = io.CopyN(io.Discard, conn, int64(skip+2))
	return err
}

func parseProviderHTML(body []byte) []result {
	seen := map[string]struct{}{}
	results := make([]result, 0)
	for _, m := range onionAnchorPattern.FindAllSubmatch(body, -1) {
		if len(m) < 3 {
			continue
		}
		target := normalizeOnionURL(string(m[1]))
		if target == "" {
			continue
		}
		title := cleanText(string(m[2]))
		if title == "" {
			title = target
		}
		appendUniqueResult(&results, seen, target, title)
	}
	for _, raw := range onionURLPattern.FindAllString(string(body), -1) {
		appendUniqueResult(&results, seen, normalizeOnionURL(raw), "Onion service")
	}
	return results
}

func appendUniqueResult(results *[]result, seen map[string]struct{}, target, title string) {
	key := canonicalResultKey(target)
	if key == "" {
		return
	}
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*results = append(*results, result{Title: title, URL: target})
}

func normalizeOnionURL(raw string) string {
	raw = strings.TrimSpace(html.UnescapeString(raw))
	if strings.HasPrefix(raw, "//") {
		raw = "http:" + raw
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return ""
	}
	p, err := url.Parse(raw)
	if err != nil || p.Hostname() == "" {
		return ""
	}
	host := strings.ToLower(p.Hostname())
	if !strings.HasSuffix(host, ".onion") || !validOnionHost(host) {
		return ""
	}
	p.Fragment = ""
	return p.String()
}

func validOnionHost(host string) bool {
	label := strings.TrimSuffix(host, ".onion")
	if len(label) != 16 && len(label) != 56 {
		return false
	}
	for _, r := range label {
		if !((r >= 'a' && r <= 'z') || (r >= '2' && r <= '7')) {
			return false
		}
	}
	return true
}

func isClearResultURL(raw string) bool {
	p, err := url.Parse(raw)
	if err != nil || p.Hostname() == "" || (p.Scheme != "http" && p.Scheme != "https") {
		return false
	}
	host := strings.ToLower(p.Hostname())
	if strings.HasSuffix(host, ".onion") || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return false
	}
	return !isPrivateOrLocalHost(host)
}

func cleanText(value string) string {
	value = tagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	value = spacePattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func canonicalResultKey(raw string) string {
	p, err := url.Parse(raw)
	if err != nil {
		return strings.ToLower(raw)
	}
	return strings.ToLower(p.Host + p.EscapedPath())
}

func scoreResult(terms []string, item result) int {
	score := 0
	title := strings.ToLower(item.Title)
	hay := strings.ToLower(item.Title + " " + item.URL + " " + item.Snippet)
	for _, term := range terms {
		term = strings.ToLower(term)
		if strings.Contains(hay, term) {
			score += 2
		}
		if strings.Contains(title, term) {
			score += 3
		}
		if strings.Contains(strings.ToLower(item.URL), term) {
			score++
		}
	}
	return score
}

func rankResults(query string, results []result) []result {
	terms := tokenPattern.FindAllString(strings.ToLower(query), -1)
	for i := range results {
		results[i].Score = scoreResult(terms, results[i])
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Title < results[j].Title
		}
		return results[i].Score > results[j].Score
	})
	return results
}

func cacheGet(key string) ([]result, bool) {
	cache.mu.RLock()
	entry, ok := cache.entries[key]
	cache.mu.RUnlock()
	if !ok || time.Since(entry.Created) > cacheTTL {
		return nil, false
	}
	return append([]result(nil), entry.Results...), true
}

func cachePut(key string, results []result) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.entries) >= 256 {
		var oldestKey string
		var oldest time.Time
		for k, e := range cache.entries {
			if oldestKey == "" || e.Created.Before(oldest) {
				oldestKey = k
				oldest = e.Created
			}
		}
		if oldestKey != "" {
			delete(cache.entries, oldestKey)
		}
	}
	cache.entries[key] = cachedSearch{Created: time.Now(), Results: append([]result(nil), results...)}
}

func writeResults(w http.ResponseWriter, query, searchType string, items []result, page, total int) {
	writeHeader(w, query, searchType, "all")
	fmt.Fprintf(w, "<main class='results'><div class='results-head'><span>%d results</span><span class='mode-pill'>%s</span></div>", total, html.EscapeString(resultTypeLabel(searchType)))
	for i, item := range items {
		fmt.Fprintf(w, "<article class='result-card'><div class='result-number'>%d</div><div class='result-body'><a class='title' href='%s' target='_blank' rel='noreferrer'>%s</a><div class='url'>%s</div>", (page-1)*defaultPageSize+i+1, html.EscapeString(item.URL), html.EscapeString(item.Title), html.EscapeString(item.URL))
		if item.Snippet != "" {
			fmt.Fprintf(w, "<p>%s</p>", html.EscapeString(item.Snippet))
		}
		fmt.Fprint(w, "</div></article>")
	}
	if total == 0 {
		fmt.Fprint(w, "<div class='empty'>No results found.</div>")
	}
	writePagination(w, query, searchType, "all", page, (total+defaultPageSize-1)/defaultPageSize)
	fmt.Fprint(w, "</main></body></html>")
}

func writeImageResults(w http.ResponseWriter, query, searchType string, items []imageResult, page int) {
	writeHeader(w, query, searchType, "images")
	start := (page - 1) * 24
	if start > len(items) {
		start = len(items)
	}
	end := start + 24
	if end > len(items) {
		end = len(items)
	}

	fmt.Fprintf(w, "<main class='images-wrap'><div class='results-head'><span>%d images</span><span class='mode-pill'>Images</span></div><div class='images-grid'>", len(items))
	for _, item := range items[start:end] {
		thumb := item.Thumbnail
		if thumb == "" {
			thumb = item.ImageURL
		}
		downloadURL := "/download?url=" + url.QueryEscape(item.ImageURL)
		fmt.Fprintf(w,
			"<article class='image-card'><a class='image-link' href='%s' target='_blank' rel='noreferrer'><img src='%s' alt='%s' loading='lazy'></a><div class='image-actions'><a href='%s' download>Download</a><a href='%s' target='_blank' rel='noreferrer'>Source</a></div><div class='image-title'>%s</div></article>",
			html.EscapeString(item.ImageURL),
			html.EscapeString(thumb),
			html.EscapeString(item.Title),
			html.EscapeString(downloadURL),
			html.EscapeString(item.SourceURL),
			html.EscapeString(item.Title))
	}
	fmt.Fprint(w, "</div>")
	if len(items) == 0 {
		fmt.Fprint(w, "<div class='empty'>No images found.</div>")
	}
	pages := (len(items) + 23) / 24
	writePagination(w, query, searchType, "images", page, pages)
	fmt.Fprint(w, "</main></body></html>")
}

func writeSearchUnavailable(w http.ResponseWriter, query, searchType, tab string) {
	writeHeader(w, query, searchType, tab)
	fmt.Fprint(w, "<main class='results'><section class='status-card'><div class='status-dot'></div><h1>Search temporarily unavailable</h1><p>Lien could not reach a search provider right now. Your search request is valid; try again in a moment.</p><a class='retry' href='/search?q=")
	fmt.Fprint(w, url.QueryEscape(query), "&type=", url.QueryEscape(searchType), "&tab=", url.QueryEscape(tab), "'>Retry search</a></section></main></body></html>")
}

func resultTypeLabel(searchType string) string {
	if searchType == "onion" {
		return ".onion sites"
	}
	return "Clear sites"
}

func downloadImage(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "Invalid image URL", http.StatusBadRequest)
		return
	}
	if isPrivateOrLocalHost(u.Hostname()) {
		http.Error(w, "Local/private destinations are not allowed", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	var client *http.Client
	if strings.HasSuffix(strings.ToLower(u.Hostname()), ".onion") {
		socksAddr := os.Getenv("LIEN_TOR_SOCKS")
		if socksAddr == "" {
			socksAddr = defaultTorSocks
		}
		transport := &http.Transport{DialContext: (&socks5Dialer{address: socksAddr}).DialContext, DisableKeepAlives: true, ForceAttemptHTTP2: false}
		client = &http.Client{Transport: transport, Timeout: 25 * time.Second}
	} else {
		client = &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			if isPrivateOrLocalHost(req.URL.Hostname()) {
				return fmt.Errorf("redirect to private/local host denied")
			}
			return nil
		}}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		http.Error(w, "Invalid image URL", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Android) AppleWebKit/537.36 Chrome/124 Safari/537.36 Lien/2.3")
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Image download failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("Image source returned HTTP %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	contentType := strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Type")))
	if !strings.HasPrefix(contentType, "image/") {
		http.Error(w, "The source did not return an image", http.StatusUnsupportedMediaType)
		return
	}
	contentLength := resp.ContentLength
	if contentLength > 15<<20 {
		http.Error(w, "Image is too large", http.StatusRequestEntityTooLarge)
		return
	}

	ext := "bin"
	if p := strings.LastIndex(u.Path, "."); p >= 0 && p+1 < len(u.Path) {
		candidate := strings.ToLower(u.Path[p+1:])
		if len(candidate) <= 8 && regexp.MustCompile(`^[a-z0-9]+$`).MatchString(candidate) {
			ext = candidate
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"lien-image.%s\"", ext))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 15<<20))
}

func isPrivateOrLocalHost(host string) bool {
	host = strings.Trim(host, "[]")
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") || lower == "127.0.0.1" || lower == "::1" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	return false
}

func writeHeader(w http.ResponseWriter, query, searchType, tab string) {
	fmt.Fprint(w, "<!doctype html><html><head><meta name='viewport' content='width=device-width,initial-scale=1'><meta name='color-scheme' content='dark'><title>Lien search</title><style>")
	fmt.Fprint(w, `:root{color-scheme:dark}*{box-sizing:border-box}html,body{min-height:100%;margin:0}body{font-family:"Segoe UI",Arial,sans-serif;background:#0a0a0a;color:#f5f5f5;padding:18px 10px 44px}.top,.results,.images-wrap{width:min(760px,100%);margin:0 auto}.top{display:flex;flex-direction:column;align-items:stretch}.brand-image{width:min(460px,100%);height:150px;object-fit:cover;display:block;margin:0 auto 12px;border-radius:20px;filter:grayscale(1) contrast(1.1);transition:filter .35s ease,transform .35s ease}.brand-image:hover{filter:grayscale(0);transform:scale(1.01)}.searchbar{position:relative;width:100%}.searchbar input[name=q]{width:100%;height:42px;border:1px solid #2a2a2a;border-radius:22px;padding:0 54px 0 16px;font-size:15px;outline:none;background:#141414;color:#ffffff !important;-webkit-text-fill-color:#ffffff !important;caret-color:#ffffff;transition:border-color .25s ease,box-shadow .25s ease}.searchbar input[name=q]::placeholder{color:#ffffff !important;opacity:1}.searchbar input[name=q]:-webkit-autofill,.searchbar input[name=q]:-webkit-autofill:hover,.searchbar input[name=q]:-webkit-autofill:focus{-webkit-text-fill-color:#ffffff !important;caret-color:#ffffff;transition:background-color 9999s ease-out 0s}.searchbar input[name=q]:focus{border-color:#555;box-shadow:0 0 0 3px rgba(245,245,245,.06)}.searchbar button{position:absolute;right:3px;top:3px;width:36px;height:36px;border:0;border-radius:50%;padding:6px;background:transparent;display:grid;place-items:center;cursor:pointer;transition:transform .2s ease,background .2s ease}.searchbar button:hover{transform:scale(1.06);background:#000}.searchbar button:active{transform:scale(.96)}.searchbar button img{width:22px;height:22px;object-fit:contain}.typebox{margin-top:10px;width:100%;height:38px;border:1px solid #2a2a2a;background:#141414;color:#f5f5f5;border-radius:20px;padding:0 14px;font-size:14px;outline:none;cursor:pointer}.tabs{display:flex;gap:20px;border-bottom:1px solid #222;margin-top:14px}.tabs a{padding:10px 2px 9px;text-decoration:none;color:#888;transition:color .25s ease}.tabs a:hover{color:#f5f5f5}.tabs a.active{color:#f5f5f5;font-weight:700;border-bottom:2px solid #f5f5f5}.mode-text{font-size:12px;color:#777;margin-top:8px}.results,.images-wrap{margin-top:12px}.results-head{display:flex;align-items:center;justify-content:space-between;gap:10px;color:#909090;font-size:13px;margin:0 0 10px}.mode-pill{border:1px solid #2a2a2a;background:#111;border-radius:999px;padding:5px 9px;color:#b5b5b5}.result-card{display:flex;gap:12px;border:1px solid #202020;background:#111;border-radius:16px;padding:14px;margin:9px 0;transition:border-color .25s ease,transform .25s ease,background .25s ease}.result-card:hover{border-color:#383838;background:#131313;transform:translateY(-1px)}.result-number{flex:0 0 28px;width:28px;height:28px;border:1px solid #292929;border-radius:50%;display:grid;place-items:center;color:#777;font-size:12px;margin-top:1px}.result-body{min-width:0;flex:1}.result .title,.result-body .title{display:block;font-size:18px;line-height:1.25;color:#f5f5f5;text-decoration:none;word-break:break-word}.result-body .title:hover{text-decoration:underline}.url{font-size:12px;color:#7fbf7f;margin-top:5px;word-break:break-all}.result-body p{margin:7px 0 0;color:#aaa;line-height:1.5;font-size:14px}.empty{padding:24px 6px;color:#777;text-align:center}.pager{display:flex;flex-wrap:wrap;align-items:center;justify-content:center;gap:7px;margin:24px 0 12px}.pager a{min-width:34px;height:34px;border:1px solid #2a2a2a;border-radius:17px;padding:0 10px;text-decoration:none;color:#aaa;display:grid;place-items:center;font-size:13px;background:#111;transition:background .2s ease,border-color .2s ease,color .2s ease}.pager a:hover{color:#f5f5f5;border-color:#444}.pager a.active{background:#f5f5f5;color:#0a0a0a;border-color:#f5f5f5}.status-card{border:1px solid #202020;background:#111;border-radius:18px;padding:24px;text-align:center;margin-top:18px}.status-dot{width:9px;height:9px;border-radius:50%;background:#888;margin:0 auto 13px}.status-card h1{font-size:21px;margin:0 0 8px}.status-card p{margin:0 auto 18px;color:#999;max-width:520px;line-height:1.5}.retry{display:inline-flex;align-items:center;justify-content:center;min-height:38px;padding:0 16px;border:1px solid #2a2a2a;border-radius:19px;background:#171717;color:#f5f5f5;text-decoration:none}.retry:hover{border-color:#555;background:#1c1c1c}.images-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.image-card{border:1px solid #202020;border-radius:16px;overflow:hidden;background:#111;transition:border-color .25s ease,transform .25s ease}.image-card:hover{border-color:#383838;transform:translateY(-1px)}.image-link{display:block;background:#080808}.image-card img{width:100%;height:180px;object-fit:cover;display:block}.image-actions{padding:9px 10px 0;display:flex;gap:12px;font-size:12px}.image-actions a{color:#aaa;text-decoration:none}.image-actions a:hover{color:#f5f5f5}.image-title{padding:7px 10px 11px;color:#999;font-size:13px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis}@media(max-width:620px){body{padding:12px 8px 34px}.brand-image{height:auto;max-height:170px}.tabs{gap:18px}.result-card{padding:12px}.images-grid{grid-template-columns:repeat(2,minmax(0,1fr));gap:9px}.image-card img{height:140px}.result-body .title{font-size:17px}.pager a{min-width:32px;height:32px}}`)
	fmt.Fprint(w, "</style></head><body><header class='top'><img class='brand-image' src='https://i.ibb.co/bgpwqfgr/Untitled60-20260918095055.png' alt='Lien'><form id='search-form' class='searchbar' action='/search' method='GET'><input name='q' value='")
	fmt.Fprint(w, html.EscapeString(query), "' placeholder='Search...' autocomplete='off'><input type='hidden' name='tab' value='")
	fmt.Fprint(w, html.EscapeString(tab), "'><button type='submit' aria-label='Search'><img src='https://i.ibb.co/r28GNFXF/Untitled56-20260917190920.png' alt=''></button></form><select class='typebox' name='type' form='search-form' aria-label='Search type'><option value='clear'")
	fmt.Fprint(w, selected(searchType == "clear"), ">Clear sites</option><option value='onion'")
	fmt.Fprint(w, selected(searchType == "onion"), ">.onion sites</option></select><nav class='tabs'><a class='", active(tab == "all"), "' href='/search?q=", url.QueryEscape(query), "&type=", url.QueryEscape(searchType), "&tab=all'>All</a><a class='", active(tab == "images"), "' href='/search?q=", url.QueryEscape(query), "&type=", url.QueryEscape(searchType), "&tab=images'>Images</a></nav><div class='mode-text'>", html.EscapeString(resultTypeLabel(searchType)), "</div></header>")
}

func selected(ok bool) string {
	if ok {
		return " selected"
	}
	return ""
}

func active(ok bool) string {
	if ok {
		return "active"
	}
	return ""
}

func writePagination(w http.ResponseWriter, q, t, tab string, page, pages int) {
	if pages < 2 {
		return
	}
	fmt.Fprint(w, "<div class='pager'>")
	if page > 1 {
		fmt.Fprintf(w, "<a href='/search?q=%s&type=%s&tab=%s&page=%d'>Previous</a>", url.QueryEscape(q), url.QueryEscape(t), tab, page-1)
	}
	for i := 1; i <= pages; i++ {
		cls := ""
		if i == page {
			cls = " class='active'"
		}
		fmt.Fprintf(w, "<a%s href='/search?q=%s&type=%s&tab=%s&page=%d'>%d</a>", cls, url.QueryEscape(q), url.QueryEscape(t), tab, i, i)
	}
	if page < pages {
		fmt.Fprintf(w, "<a href='/search?q=%s&type=%s&tab=%s&page=%d'>Next</a>", url.QueryEscape(q), url.QueryEscape(t), tab, page+1)
	}
	fmt.Fprint(w, "</div>")
}
