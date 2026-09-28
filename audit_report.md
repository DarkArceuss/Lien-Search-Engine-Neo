# Lien Neo3 verification report

- `go test ./...` passes.
- `go vet ./...` passes.
- Clean production build passes.
- Android `GOOS=android GOARCH=arm64 CGO_ENABLED=0` build passes.
- Both Termux launcher scripts pass `bash -n` syntax checks.
- The launcher was integration-tested against a mock Onion-Hoster command interface; it started the real Lien binary on `127.0.0.1:3000` and the health endpoint returned `ok`.
- A real local HTTP integration test exercised Clear sites through DuckDuckGo HTML, DuckDuckGo Lite, and Bing-style result responses; all three results were merged by the real Lien server.
- The same real-server test exercised the Tor-first/direct-fallback path by leaving `LIEN_CLEAR_VIA_TOR=1` while the test provider ran locally.
- Clear-web parsers reject `.onion`, localhost, loopback, private, link-local, and unspecified result targets.
- Provider failures no longer produce a raw `502 Search failed` page; Lien renders a styled retry page.
- Result pages use only local assets (`banner.png` and `search_icon.png`) and do not depend on the external iBB image URL.
- Result pages follow the existing Lien dark styling instead of the earlier generic light/dark result template.
- Existing onion search, image search, pagination, and download safety tests continue to pass.
