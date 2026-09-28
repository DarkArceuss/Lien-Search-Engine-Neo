# Lien

Lien is a small private search front end with a local result collector for public `.onion` search results.

## Search providers

`.onion` search uses TorDex through the local Tor SOCKS5 listener (default `127.0.0.1:9050`) plus OnionLand over HTTPS. Results are merged, deduplicated, ranked locally, cached briefly, and paginated.

`Clear sites` no longer depends on a single provider. Lien tries DuckDuckGo HTML, DuckDuckGo Lite, and Bing web results, merges successful responses, and filters `.onion` and local/private targets from the clear-web result set. When running as an Onion-Hoster service, clear-web provider requests use the local Tor SOCKS5 listener first and automatically fall back to a direct HTTP client when a provider or exit path rejects the Tor request.

## Termux / Onion Hoster

Onion Hoster's Termux Edition supports dynamic sites through the custom-port method.

```sh
cd Test10
chmod +x tools/*.sh
tools/start-onion-host.sh
```

The launcher starts Lien on `127.0.0.1:3000`, enables `LIEN_CLEAR_VIA_TOR=1`, and then starts Onion Hoster with `custom_port 3000`. Onion Hoster documents the custom-port method for dynamic web servers. 

## Direct local run

```sh
go run ./scripts
```

Open `http://127.0.0.1:3000`.

## Optional environment variables

```sh
export LIEN_TOR_SOCKS=127.0.0.1:9050
export LIEN_TORDEX_URL='http://tordexu73joywapk2txdr54jed4imqledpcvcuf75qsas2gwdgksvnyd.onion/search?query=%s&page=%d'
export LIEN_ONIONLAND_URL='https://www.onionland.to/search?q=%s&page=%d'
export LIEN_CLEAR_VIA_TOR=1
export LIEN_CLEAR_TOR_SOCKS=127.0.0.1:9050
```

Clear-web provider endpoints can also be overridden for testing with `LIEN_DDG_URL`, `LIEN_DDG_LITE_URL`, and `LIEN_BING_SEARCH_URL`.

## Search modes

The front page submits `type=clear` for normal web search and `type=onion` for onion search. Result pages provide `All` and `Images` tabs.

Clear-web results use three independent HTML search endpoints. Successful providers are merged, deduplicated, and locally ranked; a single provider failure does not make the whole clear-web search fail. If every provider is unreachable, Lien renders a styled retry page instead of returning a raw gateway-error page.

## UI updates

The home page keeps its existing visual structure. Result pages now use the same Lien dark visual language: `#0a0a0a` background, rounded dark cards, the local search icon, the existing banner, rounded selector, tabs, and numbered pagination. Image results use the same card style.

Image downloads are handled by the Lien server. Onion image URLs are fetched through the local Tor SOCKS5 listener.
