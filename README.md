# easy-drm — Clear Key video DRM in Go

Self-hosted video streaming with browser-native DRM (EME Clear Key,
`org.w3.clearkey`). The video is encrypted **once** into standard MPEG-CENC
(AES-128 CTR) — the server never re-encrypts anything and serves the file as
plain static bytes with Range support. Decryption happens inside the
browser's media pipeline (MSE + EME): decrypted frames never pass through
page JavaScript, and the key is delivered by a token-protected mini license
server.

## How it works

```
┌──────────┐   1. packager (once, offline)      ┌──────────────┐
│ input.mp4 │ ─ ffmpeg remux → MP4Box CENC ───→ │ video.mp4    │  encrypted,
└──────────┘        → MP4Box DASH fragment      │ video.mp4.json│  fragmented
                                                └──────┬───────┘
                                                       │ served as static bytes
┌─────────────────────────── browser ──────────────────▼───────────────────┐
│ GET /            → player page with signed HMAC token                    │
│ POST /license    → Clear Key license (JWK) — requires valid token        │
│ GET /video.mp4   → encrypted file (Range OK) — requires valid token      │
│                                                                          │
│ MSE SourceBuffer ← fetch stream      EME CDM ← license                   │
│ (encrypted bytes)                    (decrypts inside media pipeline)    │
└──────────────────────────────────────────────────────────────────────────┘
```

## Prerequisites

- Go 1.21+
- `ffmpeg` / `ffprobe` (remux + codec detection)
- `MP4Box` from [GPAC](https://gpac.io) (CENC encryption + MSE-compatible
  fragmentation; ffmpeg cannot write `senc` boxes into fragments)

```bash
apt install ffmpeg gpac
```

## Usage

### 1. Generate secrets

```bash
openssl rand -hex 16   # CENC_KEY
openssl rand -hex 16   # CENC_KID
openssl rand -hex 32   # TOKEN_SECRET
```

Copy `.env.example` to `.env` and fill the values in.

### 2. Package the video (once)

```bash
make build-packager
./build/packager input.mp4 data/video.mp4 $CENC_KEY $CENC_KID
```

No re-encoding happens — only container-level remux, encryption and
fragmentation. The tool also writes `data/video.mp4.json` with the exact
codec string the player needs. Run it without key/kid arguments to have
random ones generated and printed.

### 3. Run the server

```bash
docker-compose up --build
# or locally:
make build && ./build/stream
```

Open `http://localhost/` — the page embeds a short-lived signed token,
requests the key from `/license` and streams the encrypted file into MSE.

## Endpoints

| Endpoint | Method | Auth | Description |
|---|---|---|---|
| `/` | GET | — | Player page with a fresh signed token |
| `/license` | POST | token | Clear Key license (JWK Set) |
| `/video.mp4` | GET | token | CENC-encrypted video, Range supported |

Tokens are `HMAC-SHA256(hour)` signatures valid for the current and previous
hour (~1–2 h window).

## Security notes

- This raises the bar for casual downloading substantially: the file on the
  wire is encrypted, the key never appears in page source, and decrypted
  data never exists as a page-accessible Blob.
- It is **not** studio-grade DRM: Clear Key has no hardware protection, so
  a determined user with DevTools can still extract the key from the license
  response. For that threat model you need Widevine/FairPlay and a license
  service. The packaged CENC file is already compatible with that upgrade
  path.
- Always deploy behind HTTPS (EME requires a secure context anyway).
