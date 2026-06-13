package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// EDRM chunk format (produced by encoder):
//   header 16 bytes: "EDRM" + version(4) + chunkCount(4) + plainChunkSize(4)
//   per chunk: nonce(12) + encLen(4) + ciphertext(encLen bytes)

var (
	aesKey       []byte
	tokenSecret  []byte
	fileData     []byte
	chunkCount   int
	chunkOffsets []int // byte offset of each chunk entry in fileData
)

func loadConfig() {
	keyHex := os.Getenv("AES_KEY")
	if keyHex == "" {
		log.Fatal("AES_KEY env var not set (64 hex chars = 32-byte AES-256 key)")
	}
	var err error
	aesKey, err = hex.DecodeString(keyHex)
	if err != nil || len(aesKey) != 32 {
		log.Fatal("AES_KEY must be 64 hex chars (32 bytes)")
	}

	secret := os.Getenv("TOKEN_SECRET")
	if secret == "" {
		log.Fatal("TOKEN_SECRET env var not set")
	}
	tokenSecret = []byte(secret)
}

func loadVideo(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Cannot read %s: %v\n\nHint: encrypt your video first with the encoder tool:\n  encoder input.mp4 data/input.enc <AES_KEY_HEX>", path, err)
	}
	if len(data) < 16 || string(data[:4]) != "EDRM" {
		log.Fatalf("Invalid EDRM file: %s", path)
	}

	n := int(binary.LittleEndian.Uint32(data[8:12]))
	fileData = data
	chunkCount = n

	chunkOffsets = make([]int, n)
	offset := 16
	for i := 0; i < n; i++ {
		if offset+16 > len(data) {
			log.Fatalf("File truncated at chunk %d", i)
		}
		chunkOffsets[i] = offset
		encLen := int(binary.LittleEndian.Uint32(data[offset+12 : offset+16]))
		offset += 12 + 4 + encLen
	}
	log.Printf("Loaded encrypted video: %d chunks (%d bytes total)", n, len(data))
}

// Token = base64url( hourTimestamp + "." + base64url(HMAC-SHA256(hourTimestamp)) )
// Valid for current and previous hour (~up to 2 hours window).

func makeToken() string {
	hour := strconv.FormatInt(time.Now().Unix()/3600, 10)
	mac := hmac.New(sha256.New, tokenSecret)
	mac.Write([]byte(hour))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(hour + "." + sig))
}

func validToken(token string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return false
	}
	dot := strings.IndexByte(string(raw), '.')
	if dot < 0 {
		return false
	}
	hourStr := string(raw[:dot])
	gotSig := string(raw[dot+1:])

	hour, err := strconv.ParseInt(hourStr, 10, 64)
	if err != nil {
		return false
	}
	cur := time.Now().Unix() / 3600
	if hour != cur && hour != cur-1 {
		return false
	}

	mac := hmac.New(sha256.New, tokenSecret)
	mac.Write([]byte(hourStr))
	wantSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(gotSig), []byte(wantSig))
}

// ------------------------------------------------------------------
// HTML player template (embedded in binary — no static files needed)
// ------------------------------------------------------------------

var playerTmpl = template.Must(template.New("player").Parse(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="drm-token" content="{{.Token}}">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Video Player</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #111; color: #eee; font-family: sans-serif;
         display: flex; flex-direction: column; align-items: center;
         justify-content: center; min-height: 100vh; gap: 16px; }
  video { max-width: min(960px, 100vw); max-height: 80vh; background: #000; }
  #status { font-size: 14px; opacity: .8; }
  #bar { width: min(480px, 90vw); height: 6px; background: #333; border-radius: 3px; display: none; }
  #fill { height: 100%; width: 0; background: #4af; border-radius: 3px; transition: width .2s; }
</style>
</head>
<body>
<video id="v" controls></video>
<div id="status">Подготовка...</div>
<div id="bar"><div id="fill"></div></div>
<script>
(async () => {
  const TOKEN   = document.querySelector('meta[name="drm-token"]').content;
  const status  = document.getElementById('status');
  const bar     = document.getElementById('bar');
  const fill    = document.getElementById('fill');
  const video   = document.getElementById('v');

  const setProgress = (v) => { fill.style.width = (v * 100).toFixed(1) + '%'; };

  try {
    // Fetch key and meta in parallel
    const [kr, mr] = await Promise.all([
      fetch('/key?token='        + TOKEN),
      fetch('/video/meta?token=' + TOKEN),
    ]);
    if (!kr.ok) throw new Error('Ошибка авторизации (' + kr.status + ')');
    if (!mr.ok) throw new Error('Ошибка метаданных ('  + mr.status + ')');

    const { key: keyB64 }  = await kr.json();
    const { chunks }       = await mr.json();

    const keyBytes  = Uint8Array.from(atob(keyB64), c => c.charCodeAt(0));
    const cryptoKey = await crypto.subtle.importKey(
      'raw', keyBytes, { name: 'AES-GCM' }, false, ['decrypt']
    );

    // Download all chunks in parallel, track progress
    status.textContent = 'Загрузка...';
    bar.style.display = 'block';
    let done = 0;

    const encBuffers = await Promise.all(
      Array.from({ length: chunks }, (_, i) =>
        fetch('/video/chunk/' + i + '?token=' + TOKEN)
          .then(r => { if (!r.ok) throw new Error('chunk ' + i); return r.arrayBuffer(); })
          .then(buf => { setProgress(++done / chunks * 0.5); return buf; })
      )
    );

    // Decrypt chunks (browser can parallelise this too)
    status.textContent = 'Расшифровка...';
    const plains = await Promise.all(
      encBuffers.map(async (buf, i) => {
        const a      = new Uint8Array(buf);
        const nonce  = a.slice(0, 12);
        const cipher = a.slice(16); // skip nonce(12) + encLen(4)
        const plain  = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: nonce }, cryptoKey, cipher);
        setProgress(0.5 + (i + 1) / chunks * 0.5);
        return plain;
      })
    );

    // Concatenate and create blob URL
    const total    = plains.reduce((s, p) => s + p.byteLength, 0);
    const combined = new Uint8Array(total);
    let offset = 0;
    for (const p of plains) { combined.set(new Uint8Array(p), offset); offset += p.byteLength; }

    video.src = URL.createObjectURL(new Blob([combined], { type: 'video/mp4' }));
    video.play().catch(() => {});
    bar.style.display  = 'none';
    status.textContent = '';

  } catch (e) {
    status.textContent = 'Ошибка: ' + e.message;
    console.error(e);
  }
})();
</script>
</body>
</html>
`))

// ------------------------------------------------------------------

func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
}

func withToken(r *http.Request) bool {
	return validToken(r.URL.Query().Get("token"))
}

func main() {
	loadConfig()

	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	loadVideo(wd + "/data/input.enc")

	mux := http.NewServeMux()

	// Player page — embeds a fresh signed token
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		noCache(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		playerTmpl.Execute(w, map[string]string{"Token": makeToken()})
	})

	// Return AES key as base64 JSON (requires valid token)
	mux.HandleFunc("/key", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		if !withToken(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"key": base64.StdEncoding.EncodeToString(aesKey),
		})
	})

	// Return total chunk count (requires valid token)
	mux.HandleFunc("/video/meta", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		if !withToken(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"chunks": chunkCount})
	})

	// Return one encrypted chunk: nonce(12) + encLen(4) + ciphertext (requires valid token)
	mux.HandleFunc("/video/chunk/", func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		if !withToken(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		idxStr := strings.TrimPrefix(r.URL.Path, "/video/chunk/")
		n, err := strconv.Atoi(idxStr)
		if err != nil || n < 0 || n >= chunkCount {
			http.Error(w, "Invalid chunk index", http.StatusBadRequest)
			return
		}
		off := chunkOffsets[n]
		encLen := int(binary.LittleEndian.Uint32(fileData[off+12 : off+16]))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(fileData[off : off+12+4+encLen])
	})

	cors := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if origin := r.Header.Get("Origin"); origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	addr := ":8080"
	log.Printf("Listening on %s", addr)
	if err := http.ListenAndServe(addr, cors(mux)); err != nil {
		log.Fatal(err)
	}
}

