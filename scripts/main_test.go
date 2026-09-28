package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestNormalizeOnionURL(t *testing.T) {
	valid := "http://abcdefghijklmnop.onion/path#fragment"
	got := normalizeOnionURL(valid)
	want := "http://abcdefghijklmnop.onion/path"
	if got != want {
		t.Fatalf("normalizeOnionURL() = %q, want %q", got, want)
	}
	if normalizeOnionURL("https://example.com") != "" {
		t.Fatal("clearnet URL should be rejected")
	}
}

func TestParseProviderHTML(t *testing.T) {
	html := []byte(`<a href="http://abcdefghijklmnop.onion/">Example One</a><a href="https://abcdefghijklmnop.onion/">Duplicate</a><a href="https://example.com/">Nope</a>`)
	results := parseProviderHTML(html)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Title != "Example One" {
		t.Fatalf("got title %q", results[0].Title)
	}
}

func TestParseProviderHTMLFindsBareOnionURL(t *testing.T) {
	html := []byte(`Some result: https://abcdefghijklmnop.onion/test?q=1`)
	results := parseProviderHTML(html)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].URL != "https://abcdefghijklmnop.onion/test?q=1" {
		t.Fatalf("got URL %q", results[0].URL)
	}
}

func TestScoreResult(t *testing.T) {
	top := result{Title: "Tor Search Portal", URL: "http://abcdefghijklmnop.onion/"}
	low := result{Title: "Unrelated", URL: "http://qrstuvwxyzabcdef.onion/"}
	if scoreResult([]string{"tor"}, top) <= scoreResult([]string{"tor"}, low) {
		t.Fatal("matching title should rank higher")
	}
}

func TestEmbeddedFrontPageCSS(t *testing.T) {
	data, err := os.ReadFile("../index.html")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, `href="styles/style.css"`) {
		t.Fatal("front page still references external style.css")
	}
	if !strings.Contains(text, "<style>") || !strings.Contains(text, "background: #0a0a0a") {
		t.Fatal("embedded original CSS was not found")
	}
}

func TestParseDuckDuckGoHTML(t *testing.T) {
	h := []byte(`<div class="result"><a class="result__a" href="https://example.org/path">Example Site</a><a class="result__snippet">Example description</a></div>`)
	results := parseDuckDuckGoHTML(h)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].URL != "https://example.org/path" || results[0].Title != "Example Site" {
		t.Fatalf("unexpected result: %#v", results[0])
	}
}

func TestParseDuckDuckGoFiltersOnionAndPrivateURLs(t *testing.T) {
	h := []byte(`<div class="result"><a class="result__a" href="https://abcdefghijklmnop.onion/">Onion</a></div><div class="result"><a class="result__a" href="http://127.0.0.1/test">Local</a></div><div class="result"><a class="result__a" href="https://example.org/ok">Clear</a></div>`)
	results := parseDuckDuckGoHTML(h)
	if len(results) != 1 || results[0].URL != "https://example.org/ok" {
		t.Fatalf("unexpected clear-web results: %#v", results)
	}
}

func TestParseDuckDuckGoLiteHTML(t *testing.T) {
	h := []byte(`<a href="https://example.org/one">Example One</a><a href="https://example.org/two">Example Two</a><a href="https://abcdefghijklmnop.onion/">Onion</a>`)
	results := parseDuckDuckGoLiteHTML(h)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Example One" || results[1].URL != "https://example.org/two" {
		t.Fatalf("unexpected lite results: %#v", results)
	}
}

func TestParseBingSearchHTML(t *testing.T) {
	h := []byte(`<ol id="b_results"><li class="b_algo"><h2><a href="https://example.org/path">Example Site</a></h2><p>Example description</p></li><li class="b_algo"><h2><a href="http://test.example/page">Test Site</a></h2><p>Test description</p></li><li class="b_algo"><h2><a href="https://abcdefghijklmnop.onion/">Onion</a></h2></li></ol>`)
	results := parseBingSearchHTML(h)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].Title != "Example Site" || results[0].Snippet != "Example description" {
		t.Fatalf("unexpected bing result: %#v", results[0])
	}
}

func TestClearEndpointFormatting(t *testing.T) {
	got, err := fmtEndpoint("https://example.test/search?q=%s&page=%d", "hello world", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "q=hello+world") || !strings.Contains(got, "page=2") {
		t.Fatalf("unexpected formatted endpoint: %s", got)
	}
}

func TestSearchClearMergesWorkingProviders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ddg":
			fmt.Fprint(w, `<div class="result"><a class="result__a" href="https://example.org/ddg">DDG result</a><a class="result__snippet">DDG snippet</a></div>`)
		case "/lite":
			fmt.Fprint(w, `<a href="https://example.org/lite">Lite result</a>`)
		case "/bing":
			fmt.Fprint(w, `<li class="b_algo"><h2><a href="https://example.org/bing">Bing result</a></h2><p>Bing snippet</p></li>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	oldVia, hadVia := os.LookupEnv("LIEN_CLEAR_VIA_TOR")
	oldDDG, hadDDG := os.LookupEnv("LIEN_DDG_URL")
	oldLite, hadLite := os.LookupEnv("LIEN_DDG_LITE_URL")
	oldBing, hadBing := os.LookupEnv("LIEN_BING_SEARCH_URL")
	defer func() {
		restoreEnv("LIEN_CLEAR_VIA_TOR", oldVia, hadVia)
		restoreEnv("LIEN_DDG_URL", oldDDG, hadDDG)
		restoreEnv("LIEN_DDG_LITE_URL", oldLite, hadLite)
		restoreEnv("LIEN_BING_SEARCH_URL", oldBing, hadBing)
	}()
	_ = os.Setenv("LIEN_CLEAR_VIA_TOR", "0")
	_ = os.Setenv("LIEN_DDG_URL", server.URL+"/ddg?q=%s&s=%d")
	_ = os.Setenv("LIEN_DDG_LITE_URL", server.URL+"/lite?q=%s")
	_ = os.Setenv("LIEN_BING_SEARCH_URL", server.URL+"/bing?q=%s&first=%d&count=30")

	results, err := searchClear(context.Background(), "cats")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d clear results, want 3: %#v", len(results), results)
	}
}

func TestSearchClearFailureRendersFriendlyPage(t *testing.T) {
	oldVia, hadVia := os.LookupEnv("LIEN_CLEAR_VIA_TOR")
	oldDDG, hadDDG := os.LookupEnv("LIEN_DDG_URL")
	oldLite, hadLite := os.LookupEnv("LIEN_DDG_LITE_URL")
	oldBing, hadBing := os.LookupEnv("LIEN_BING_SEARCH_URL")
	defer func() {
		restoreEnv("LIEN_CLEAR_VIA_TOR", oldVia, hadVia)
		restoreEnv("LIEN_DDG_URL", oldDDG, hadDDG)
		restoreEnv("LIEN_DDG_LITE_URL", oldLite, hadLite)
		restoreEnv("LIEN_BING_SEARCH_URL", oldBing, hadBing)
	}()
	_ = os.Setenv("LIEN_CLEAR_VIA_TOR", "0")
	_ = os.Setenv("LIEN_DDG_URL", "http://127.0.0.1:1/ddg?q=%s&s=%d")
	_ = os.Setenv("LIEN_DDG_LITE_URL", "http://127.0.0.1:1/lite?q=%s")
	_ = os.Setenv("LIEN_BING_SEARCH_URL", "http://127.0.0.1:1/bing?q=%s&first=%d&count=30")

	r := httptest.NewRequest(http.MethodGet, "/search?q=cats&type=clear", nil)
	rr := httptest.NewRecorder()
	search(rr, r)
	if rr.Code != http.StatusOK {
		t.Fatalf("got status %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"Search temporarily unavailable", "Retry search", "background:#0a0a0a", "color:#ffffff !important"} {
		if !strings.Contains(body, want) {
			t.Fatalf("friendly failure page missing %q", want)
		}
	}
}

func restoreEnv(key, old string, had bool) {
	if had {
		_ = os.Setenv(key, old)
	} else {
		_ = os.Unsetenv(key)
	}
}

func TestClearWebClientsCanUseDirectMode(t *testing.T) {
	old, had := os.LookupEnv("LIEN_CLEAR_VIA_TOR")
	_ = os.Setenv("LIEN_CLEAR_VIA_TOR", "0")
	defer func() {
		if had {
			_ = os.Setenv("LIEN_CLEAR_VIA_TOR", old)
		} else {
			_ = os.Unsetenv("LIEN_CLEAR_VIA_TOR")
		}
	}()
	clients := clearWebClients()
	if len(clients) != 1 {
		t.Fatalf("got %d clients, want 1 direct client", len(clients))
	}
}

func TestWriteHeaderUsesLienSearchIconAndStyle(t *testing.T) {
	rr := httptest.NewRecorder()
	writeHeader(rr, "cats", "clear", "all")
	body := rr.Body.String()
	for _, want := range []string{
		"id='search-form'",
		"src='https://i.ibb.co/r28GNFXF/Untitled56-20260917190920.png'",
		"background:#0a0a0a",
		"color:#ffffff !important",
		"name='type' form='search-form'",
		"class='brand-image'",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("header missing %q", want)
		}
	}
}

func TestWritePaginationHasNumberedPages(t *testing.T) {
	rr := httptest.NewRecorder()
	writePagination(rr, "cats", "clear", "all", 2, 4)
	body := rr.Body.String()
	for _, want := range []string{">Previous<", ">1<", ">2<", ">3<", ">4<", ">Next<"} {
		if !strings.Contains(body, want) {
			t.Fatalf("pagination missing %q", want)
		}
	}
}

func TestWriteSearchUnavailableKeepsLienStyle(t *testing.T) {
	rr := httptest.NewRecorder()
	writeSearchUnavailable(rr, "cats", "clear", "all")
	body := rr.Body.String()
	for _, want := range []string{"Search temporarily unavailable", "Retry search", "background:#0a0a0a", "color:#ffffff !important"} {
		if !strings.Contains(body, want) {
			t.Fatalf("unavailable page missing %q", want)
		}
	}
}

func TestDownloadRejectsLocalTargets(t *testing.T) {
	r := httptest.NewRequest("GET", "/download?url=http://127.0.0.1:3000/x.jpg", nil)
	rr := httptest.NewRecorder()
	downloadImage(rr, r)
	if rr.Code != 403 {
		t.Fatalf("got status %d, want 403", rr.Code)
	}
}

func TestQueryInputsUseWhiteText(t *testing.T) {
	rr := httptest.NewRecorder()
	writeHeader(rr, "cats", "clear", "all")
	body := rr.Body.String()
	for _, want := range []string{
		"input[name=q]",
		"color:#ffffff !important",
		"-webkit-text-fill-color:#ffffff !important",
		"caret-color:#ffffff",
		"input[name=q]::placeholder",
		"color:#ffffff !important",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("result query field missing %q", want)
		}
	}

	home, err := os.ReadFile("../index.html")
	if err != nil {
		t.Fatal(err)
	}
	homeBody := string(home)
	for _, want := range []string{
		".container1 input {",
		"color: #ffffff !important;",
		"-webkit-text-fill-color: #ffffff !important;",
		".container1 input::placeholder",
	} {
		if !strings.Contains(homeBody, want) {
			t.Fatalf("homepage query field missing %q", want)
		}
	}
}
