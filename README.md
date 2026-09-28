# Lien Search Engine Neo
**Lien** — simple and private search engine for **Tor** and clear sites, based on [this local onion hoster](https://github.com/uzairdeveloper223/Onion-Hoster). [First version of Lien](https://github.com/DarkArceuss/Lien-tor-search-engine)

## How to install and run Lien on Termux?

1. Install Termux ([F-Droid](https://f-droid.org/packages/com.termux/))
2. Install onion hoster and run it:
```bash
termux-setup-storage
```

```bash
pkg update && pkg upgrade -y
```


```bash
git clone https://github.com/uzairdeveloper223/Onion-Hoster
```

```bash
cd Onion-hoster
```

```bash
chmod +x termux.sh
```

```bash
bash termux.sh install all
```

3. Install Lien:

```bash
git clone https://github.com/DarkArceuss/Lien-Search-Engine-Neo
```

4. In **Onion-hoster** select out repo with **Lien**:

```bash
bash termux.sh config set site_directory ~/Lien-Search-Engine-Neo/
```

5. Set this port: **3000**:

```bash
bash termux.sh method custom_port 3000
```

6. In other tab install and run **goland server**:

```bash
pkg install goland -y
```

```bash
cd ~/Lien-Search-Engine-Neo
```

```bash
go run ./scripts
```

7. Run onion host:

```bash
bash termux.sh start
```

8. If you want stop it run it:

```bash
bash termux.sh stop
```

## Credits

[Telegram](t.me/booink1)