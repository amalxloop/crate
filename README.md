# Crate — Self-Hosted Music Streaming Server

A lightweight, self-hostable music streaming server. Single binary, SQLite,Desktop-first, can run on a Raspberry Pi.

## Features

- **OpenSubsonic API** — works with Symfonium, DSub, Feishin, and other Subsonic clients
- **Smart Playlists** — visual rule builder
- **YouTube download** — paste a URL, Crate downloads and indexes it
- **Auto-metadata** — fetches artist, album, cover art from YouTube and file tags
- **Transcoding** — on-the-fly FFmpeg transcoding
- **Scrobbling** — Last.fm and ListenBrainz
- **Web UI** — responsive PWA with audio visualizer
- **Multi-user** — independent playlists, ratings, play history
- **File watcher** — auto-indexes new files dropped into your music folder
- **Gapless playback** and ReplayGain

## Requirements

- Go 1.24+ (for building from source)
- FFmpeg
- yt-dlp (optional, for YouTube downloads)
- SQLite (bundled via CGO)

## Quick Start

### Docker (recommended)

```bash
git clone https://github.com/youruser/crate.git
cd crate
```

Edit `docker-compose.yml` — replace `/path/to/music` with your actual music folder:

```yaml
volumes:
  - /home/you/Music:/music:ro
```

```bash
docker compose up -d
```

Open http://localhost:4533, login with `admin` / `admin`.

### From source

```bash
# Install dependencies (Ubuntu/Debian)
sudo apt install ffmpeg

# Optional: install yt-dlp for YouTube downloads
sudo apt install yt-dlp
# or
pip install yt-dlp

# Build
go build -o crate ./cmd/crate

# Run (scans ~/Music by default)
./crate
```

### Binary release

Download the binary for your platform and run:

```bash
chmod +x crate
./crate -admin-user admin -admin-password yourpassword
```

## Commands

```
./crate [flags]

  -config string        path to config file (JSON)
  -port string          server port (default "4533")
  -admin-user string    admin username (default "admin")
  -admin-password string  admin password (generated if empty)
```

### Examples

```bash
# Default — scans ~/Music, listens on :4533
./crate

# Custom config
./crate -config /etc/crate/crate.json

# Custom port and credentials
./crate -port 8080 -admin-user admin -admin-password secretpass

# With Docker
docker compose up -d

# Rebuild after changes
go build -o crate ./cmd/crate && ./crate

# View logs (Docker)
docker compose logs -f crate

# Stop
docker compose down
```

## Configuration

Create a `crate.json`:

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 4533,
    "base_url": "/"
  },
  "database": {
    "path": "/data/crate.db"
  },
  "library": {
    "paths": ["/home/you/Music"],
    "scan_interval_minutes": 5
  },
  "transcode": {
    "default_bitrate": 192,
    "max_concurrency": 3,
    "ffmpeg_path": "ffmpeg"
  },
  "scrobble": {
    "lastfm": {
      "enabled": false,
      "api_key": "",
      "api_secret": ""
    },
    "listenbrainz": {
      "enabled": true,
      "base_url": "https://api.listenbrainz.org"
    }
  }
}
```

### Supported audio formats

FLAC, MP3, M4A, AAC, OGG, Opus, WAV, ALAC, WebM (audio), WMV (audio tracks)

## Data locations

| Path | Contents |
|------|----------|
| `~/.crate/crate.db` | SQLite database |
| `~/.crate/covers/` | Extracted cover art thumbnails |
| `~/.crate/transcode-cache/` | Cached transcodes |

## Client compatibility

| Client | Platform | Protocol |
|--------|----------|----------|
| Symfonium | Android | Subsonic |
| DSub | Android | Subsonic |
| Feishin | Desktop | Subsonic |
| Ultrasonic | Android | Subsonic |
| play:Sub | iOS | Subsonic |
| Amperfy | iOS | Subsonic |

## API endpoints

### Subsonic (for third-party clients)

All standard endpoints at `/rest/*.view` — ping, getArtists, getAlbumList, getAlbum, getSong, stream, download, getCoverArt, scrobble, star/unstar, search, playlists.

### Native REST API

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/artists` | List all artists |
| GET | `/api/artist/:id` | Artist detail + albums |
| GET | `/api/albums` | List all albums |
| GET | `/api/album/:id` | Album detail + tracks |
| GET | `/api/search?q=` | Search artists, albums, tracks |
| GET | `/api/playlists` | User playlists |
| POST | `/api/playlists` | Create playlist |
| GET | `/api/playlists/:id/tracks` | Playlist tracks |
| POST | `/api/playlists/:id/tracks` | Add track to playlist |
| DELETE | `/api/playlists/:id/tracks/:trackId` | Remove track |
| POST | `/api/favorites/:trackId` | Star a track |
| DELETE | `/api/favorites/:trackId` | Unstar a track |
| GET | `/api/favorites` | List starred tracks |
| POST | `/api/scrobble/:trackId` | Scrobble a track |
| GET | `/api/stats` | Library stats |
| GET | `/api/recently-added` | Recently added tracks |
| GET | `/api/recently-played` | Recently played tracks |
| GET | `/api/most-played` | Most played tracks |
| POST | `/api/download` | Download from YouTube |
| GET | `/api/download/:id/status` | Check download status |
| GET | `/cover/:id` | Cover art (album/track) |
| GET | `/cover/alb/:id` | Cover art (album only) |
| GET | `/stream` | Stream audio |

## Architecture

```
crate/
├── cmd/crate/main.go          # HTTP server, routes, handlers
├── internal/
│   ├── auth/                  # bcrypt, session cookies, Subsonic token auth, API keys
│   ├── config/                # JSON config loader
│   ├── db/                    # SQLite, migrations, queries
│   ├── model/                 # data models
│   └── scanner/               # file scanner, metadata extraction, cover art
├── web/templates/index.html   # SPA frontend
├── web/static/                # static assets
├── Dockerfile                 # multi-stage Alpine build
└── docker-compose.yml
```

## License

AGPL-3.0
