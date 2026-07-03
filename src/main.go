package main

// DRM-сервер на базе EME Clear Key (org.w3.clearkey).
//
// Видео один раз шифруется в MPEG-CENC утилитой packager (обёртка над ffmpeg,
// без перекодирования) и лежит на диске как data/video.mp4. Сервер отдаёт его
// как обычную статику с поддержкой Range (перемотка работает из коробки),
// а ключ выдаёт через мини-«лицензионный сервер» /license по подписанному токену.
// Дешифровка происходит внутри медиастека браузера — расшифрованные байты
// не проходят через JS страницы.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	cencKey     []byte // 16 байт — AES-128 ключ CENC
	cencKID     []byte // 16 байт — key ID
	tokenSecret []byte
	videoPath   string
	videoMime   string // MIME с codecs для MediaSource.addSourceBuffer
)

func mustHex16(env string) []byte {
	v := os.Getenv(env)
	if v == "" {
		log.Fatalf("%s env var not set (32 hex chars = 16 bytes)", env)
	}
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != 16 {
		log.Fatalf("%s must be 32 hex chars (16 bytes)", env)
	}
	return b
}

func loadConfig() {
	cencKey = mustHex16("CENC_KEY")
	cencKID = mustHex16("CENC_KID")

	secret := os.Getenv("TOKEN_SECRET")
	if secret == "" {
		log.Fatal("TOKEN_SECRET env var not set")
	}
	tokenSecret = []byte(secret)
}

// ------------------------------------------------------------------
// Токены: base64url( hour + "." + base64url(HMAC-SHA256(hour)) ),
// действительны в текущий и предыдущий час.
// ------------------------------------------------------------------

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

func withToken(r *http.Request) bool {
	return validToken(r.URL.Query().Get("token"))
}

// ------------------------------------------------------------------
// Плеер (встроен в бинарник)
// ------------------------------------------------------------------

var playerTmpl = template.Must(template.New("player").Parse(`<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Video Player</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #111; color: #eee; font-family: sans-serif;
         display: flex; flex-direction: column; align-items: center;
         justify-content: center; min-height: 100vh; gap: 16px; }
  video { max-width: min(960px, 100vw); max-height: 80vh; background: #000; }
  #status { font-size: 14px; opacity: .8; min-height: 1em; }
</style>
</head>
<body>
<video id="v" controls></video>
<div id="status"></div>
<script>
(async () => {
  const TOKEN = {{.Token}};
  const KID   = {{.KID}};  // base64url
  const MIME  = {{.Mime}}; // например: video/mp4; codecs="avc1.64001F, mp4a.40.2"
  const video  = document.getElementById('v');
  const status = document.getElementById('status');

  try {
    // --- EME Clear Key ---
    const access = await navigator.requestMediaKeySystemAccess('org.w3.clearkey', [
      { initDataTypes: ['keyids', 'cenc'], videoCapabilities: [{ contentType: MIME }] },
    ]);
    const mediaKeys = await access.createMediaKeys();
    await video.setMediaKeys(mediaKeys);

    const session = mediaKeys.createSession();
    session.addEventListener('message', async (e) => {
      // e.message — запрос лицензии от CDM браузера; ответ — JWK Set с ключом
      const res = await fetch('/license?token=' + TOKEN, { method: 'POST', body: e.message });
      if (!res.ok) {
        status.textContent = 'Ошибка лицензии (' + res.status + ')';
        return;
      }
      await session.update(await res.arrayBuffer());
    });

    // KID известен заранее — запрашиваем ключ явно, не дожидаясь события
    // encrypted (ffmpeg не пишет PSSH-бокс в MP4).
    await session.generateRequest('keyids',
      new TextEncoder().encode(JSON.stringify({ kids: [KID] })));

    // --- MSE: браузеры поддерживают EME только через MediaSource ---
    if (!MediaSource.isTypeSupported(MIME)) {
      throw new Error('Браузер не поддерживает ' + MIME);
    }
    const ms = new MediaSource();
    video.src = URL.createObjectURL(ms);
    await new Promise(r => ms.addEventListener('sourceopen', r, { once: true }));
    const sb = ms.addSourceBuffer(MIME);

    const appendDone = () => new Promise((resolve, reject) => {
      sb.addEventListener('updateend', resolve, { once: true });
      sb.addEventListener('error', () => reject(new Error('SourceBuffer error')), { once: true });
    });

    const append = async (buf) => {
      for (;;) {
        try {
          sb.appendBuffer(buf);
        } catch (e) {
          if (e.name === 'QuotaExceededError') {
            // Буфер полон: выкидываем уже просмотренное и ждём прогресса
            const keep = Math.max(0, video.currentTime - 10);
            if (keep > 0 && sb.buffered.length && sb.buffered.start(0) < keep) {
              sb.remove(sb.buffered.start(0), keep);
              await appendDone();
            } else {
              await new Promise(r => setTimeout(r, 1000));
            }
            continue;
          }
          throw e;
        }
        await appendDone();
        return;
      }
    };

    const resp = await fetch('/video.mp4?token=' + TOKEN);
    if (!resp.ok) throw new Error('Ошибка загрузки видео (' + resp.status + ')');

    video.play().catch(() => {}); // autoplay может быть заблокирован — не страшно

    const reader = resp.body.getReader();
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      await append(value);
    }
    if (ms.readyState === 'open') ms.endOfStream();
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

func main() {
	loadConfig()

	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	videoPath = wd + "/data/video.mp4"
	if _, err := os.Stat(videoPath); err != nil {
		log.Fatalf("Cannot open %s: %v\n\nHint: package your video first:\n  packager input.mp4 data/video.mp4 $CENC_KEY $CENC_KID", videoPath, err)
	}

	// Метаданные с MIME/codecs пишет packager рядом с видео
	metaRaw, err := os.ReadFile(videoPath + ".json")
	if err != nil {
		log.Fatalf("Cannot read %s.json: %v (produced by the packager)", videoPath, err)
	}
	var meta struct {
		Mime string `json:"mime"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil || meta.Mime == "" {
		log.Fatalf("Invalid %s.json: %v", videoPath, err)
	}
	videoMime = meta.Mime
	log.Printf("Serving CENC-encrypted video: %s (%s)", videoPath, videoMime)

	kidB64 := base64.RawURLEncoding.EncodeToString(cencKID)

	mux := http.NewServeMux()

	// Страница плеера — со свежим подписанным токеном
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		playerTmpl.Execute(w, map[string]string{
			"Token": makeToken(),
			"KID":   kidB64,
			"Mime":  videoMime,
		})
	})

	// Clear Key license: CDM присылает {"kids":[...]}, отвечаем JWK Set
	mux.HandleFunc("/license", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !withToken(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		var req struct {
			KIDs []string `json:"kids"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "Bad license request", http.StatusBadRequest)
			return
		}
		requested := false
		for _, k := range req.KIDs {
			if k == kidB64 {
				requested = true
				break
			}
		}
		if !requested {
			http.Error(w, "Unknown key ID", http.StatusForbidden)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"keys": []map[string]string{{
				"kty": "oct",
				"kid": kidB64,
				"k":   base64.RawURLEncoding.EncodeToString(cencKey),
			}},
			"type": "temporary",
		})
	})

	// Зашифрованный CENC-файл; http.ServeFile даёт Range из коробки — перемотка работает
	mux.HandleFunc("/video.mp4", func(w http.ResponseWriter, r *http.Request) {
		if !withToken(r) {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		http.ServeFile(w, r, videoPath)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("Listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
