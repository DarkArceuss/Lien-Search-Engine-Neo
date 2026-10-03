# Lien Search Engine Neo

![Version](https://img.shields.io/badge/version-8.0.0-blue)
![Go](https://img.shields.io/badge/go-1.21%2B-00ADD8)
![Platform](https://img.shields.io/badge/platform-Termux-green)
![License](https://img.shields.io/badge/license-MIT-blue)

![Tests](https://img.shields.io/badge/tests-passing-brightgreen)
![Build](https://img.shields.io/badge/build-passing-brightgreen)
![Coverage](https://img.shields.io/badge/coverage-core%20logic-yellow)
![Vet](https://img.shields.io/badge/go%20vet-clean-brightgreen)
![Code Style](https://img.shields.io/badge/code%20style-gofmt-blueviolet)

**Lien** — simple and private search engine for **Tor** (.onion) and clear sites. Lightweight Go backend, no external dependencies, dark minimal UI.

Based on [Onion-Hoster](https://github.com/uzairdeveloper223/Onion-Hoster). First version: [Lien-tor-search-engine](https://github.com/DarkArceuss/Lien-tor-search-engine).

## Features

- Clearnet search via DuckDuckGo and Bing with automatic failover
- `.onion` search via TorDex (Tor SOCKS5) and OnionLand
- Merged, deduplicated, locally ranked and cached results
- Image search with server-side download proxy
- Dark minimal UI, mobile friendly

## Requirements

- Go 1.21+
- Tor SOCKS5 proxy (default `127.0.0.1:9050`) — only for `.onion` mode

## Install and Run (Termux)

1. Install [Termux from F-Droid](https://f-droid.org/packages/com.termux/)
2. Install and configure [Onion-Hoster](https://github.com/uzairdeveloper223/Onion-Hoster):
   ```bash
   pkg update && pkg upgrade -y
   git clone https://github.com/uzairdeveloper223/Onion-Hoster
   cd Onion-Hoster && chmod +x termux.sh
   bash termux.sh install all
   bash termux.sh config set site_directory ~/Lien-Search-Engine-Neo/
   bash termux.sh method custom_port 3000
   ```
3. Clone and run Lien (in a second tab):
   ```bash
   pkg install golang -y
   git clone https://github.com/DarkArceuss/Lien-Search-Engine-Neo
   cd ~/Lien-Search-Engine-Neo
   go run ./scripts
   ```
4. Start the onion host: `bash termux.sh start` (stop: `bash termux.sh stop`)

Open `http://127.0.0.1:3000`.

## Tests

```bash
go test ./scripts
```

| Check | Status |
|---|---|
| Unit tests | ![Tests](https://img.shields.io/badge/tests-passing-brightgreen) |
| Build | ![Build](https://img.shields.io/badge/build-passing-brightgreen) |
| Go vet | ![Vet](https://img.shields.io/badge/go%20vet-clean-brightgreen) |

## Screenshots

![lien search](https://i.ibb.co/zHn55SLJ/snapix-app-IMG-20260929-105755-mockup.png)

![lien](https://files.catbox.moe/1km3e3.png)

## Configuration

| Variable | Default | Description |
|---|---|---|
| `LIEN_BIND` | `127.0.0.1:3000` | Listen address |
| `LIEN_TOR_SOCKS` | `127.0.0.1:9050` | Tor SOCKS5 proxy |
| `LIEN_CLEAR_VIA_TOR` | `1` | Route clearnet through Tor |
| `LIEN_TORDEX_URL` | built-in | Override TorDex endpoint |
| `LIEN_ONIONLAND_URL` | built-in | Override OnionLand endpoint |

## Credits

[![Telegram](https://img.shields.io/badge/Telegram-booink1-26A5E4?logo=telegram&logoColor=white)](https://t.me/booink1)
[![GitHub](https://img.shields.io/badge/GitHub-DarkArceuss-181717?logo=github&logoColor=white)](https://github.com/DarkArceuss)
[![Issues](https://img.shields.io/badge/Issues-report%20bug-red)](https://github.com/DarkArceuss/Lien-Search-Engine-Neo/issues)
