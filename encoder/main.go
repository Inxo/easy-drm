package main

// packager — переупаковывает видео в зашифрованный MPEG-CENC fragmented MP4
// (AES-128 CTR) без перекодирования. Результат воспроизводится в браузере
// через MSE + EME Clear Key (org.w3.clearkey).
//
// Пайплайн:
//   1. ffprobe  — определение кодеков (MIME для MediaSource.addSourceBuffer)
//   2. ffmpeg   — ремукс в MP4 при необходимости (без перекодирования)
//   3. MP4Box   — CENC-шифрование (ffmpeg не умеет писать senc во фрагменты)
//   4. MP4Box   — DASH onDemand: MSE-совместимый fragmented MP4 одним файлом
//
// Кроме самого файла пишет рядом <output>.json с MIME-строкой для плеера.
//
// Требует установленные ffmpeg, ffprobe и MP4Box (gpac).
//
// Usage:
//   packager <input> <output.mp4> [key_hex_32] [kid_hex_32]
//
// Если key/kid не заданы — генерируются случайные и печатаются в stdout.
// key — 16 байт (32 hex-символа, AES-128), kid — 16-байтовый идентификатор ключа.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func randomHex16() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		fatal("Failed to generate random bytes:", err)
	}
	return hex.EncodeToString(b)
}

func validHex16(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16
}

func fatal(args ...interface{}) {
	fmt.Fprintln(os.Stderr, args...)
	os.Exit(1)
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Println("+", cmd.String())
	if err := cmd.Run(); err != nil {
		fatal(name, "failed:", err)
	}
}

// ------------------------------------------------------------------
// Определение RFC 6381 codec-строки через ffprobe
// ------------------------------------------------------------------

type probeStream struct {
	CodecType string `json:"codec_type"`
	CodecName string `json:"codec_name"`
	Profile   string `json:"profile"`
	Level     int    `json:"level"`
}

var h264Profiles = map[string]string{
	"Baseline":             "4200",
	"Constrained Baseline": "42E0",
	"Main":                 "4D40",
	"Extended":             "5800",
	"High":                 "6400",
	"High 10":              "6E00",
	"High 4:2:2":           "7A00",
	"High 4:4:4":           "F400",
}

func codecString(s probeStream) string {
	switch s.CodecName {
	case "h264":
		pp, ok := h264Profiles[s.Profile]
		if !ok {
			pp = "42E0"
		}
		level := s.Level
		if level <= 0 {
			level = 30
		}
		return fmt.Sprintf("avc1.%s%02X", pp, level)
	case "hevc":
		return "hvc1.1.6.L93.B0"
	case "vp9":
		return "vp09.00.10.08"
	case "av1":
		return "av01.0.04M.08"
	case "aac":
		if strings.Contains(s.Profile, "HE") {
			return "mp4a.40.5"
		}
		return "mp4a.40.2"
	case "opus":
		return "opus"
	case "mp3":
		return "mp3"
	default:
		return ""
	}
}

func probeStreams(input string) []probeStream {
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-print_format", "json",
		"-show_streams",
		input,
	).Output()
	if err != nil {
		fatal("ffprobe failed:", err)
	}
	var probe struct {
		Streams []probeStream `json:"streams"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		fatal("ffprobe output parse error:", err)
	}

	var av []probeStream
	for _, s := range probe.Streams {
		if s.CodecType == "video" || s.CodecType == "audio" {
			av = append(av, s)
		}
	}
	if len(av) == 0 {
		fatal("no audio/video streams found in", input)
	}
	return av
}

func detectMime(streams []probeStream) string {
	var codecs []string
	for _, s := range streams {
		c := codecString(s)
		if c == "" {
			fatal("unsupported codec:", s.CodecName, "(re-encode to h264/aac first)")
		}
		codecs = append(codecs, c)
	}
	return fmt.Sprintf(`video/mp4; codecs="%s"`, strings.Join(codecs, ", "))
}

// ------------------------------------------------------------------

func main() {
	if len(os.Args) < 3 || len(os.Args) > 5 {
		fmt.Fprintln(os.Stderr, "Usage: packager <input> <output.mp4> [key_hex_32] [kid_hex_32]")
		fmt.Fprintln(os.Stderr, "  key/kid — 32 hex chars (16 bytes) each; generated randomly if omitted")
		os.Exit(1)
	}
	input, output := os.Args[1], os.Args[2]

	for _, bin := range []string{"ffmpeg", "ffprobe", "MP4Box"} {
		if _, err := exec.LookPath(bin); err != nil {
			fatal(bin, "not found in PATH — install ffmpeg and gpac first")
		}
	}

	key, kid := randomHex16(), randomHex16()
	if len(os.Args) > 3 {
		key = os.Args[3]
	}
	if len(os.Args) > 4 {
		kid = os.Args[4]
	}
	if !validHex16(key) || !validHex16(kid) {
		fatal("key and kid must each be exactly 32 hex chars (16 bytes)")
	}

	streams := probeStreams(input)
	mime := detectMime(streams)
	fmt.Println("Detected MIME:", mime)

	tmpDir, err := os.MkdirTemp("", "packager-*")
	if err != nil {
		fatal("Failed to create temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. Ремукс в MP4 без перекодирования (нормализует и не-mp4 контейнеры)
	remuxed := filepath.Join(tmpDir, "remuxed.mp4")
	run("ffmpeg", "-y", "-v", "error",
		"-i", input,
		"-map", "0:v?", "-map", "0:a?",
		"-c", "copy",
		"-f", "mp4",
		remuxed,
	)

	// 2. CENC-шифрование всех дорожек через MP4Box
	var tracks strings.Builder
	for i := range streams {
		fmt.Fprintf(&tracks,
			`  <CrypTrack trackID="%d" IsEncrypted="1" IV_size="16" first_IV="0x%s" saiSavedBox="senc">
    <key KID="0x%s" value="0x%s"/>
  </CrypTrack>
`, i+1, randomHex16(), kid, key)
	}
	drmXML := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<GPACDRM type="CENC AES-CTR">
%s</GPACDRM>
`, tracks.String())
	drmPath := filepath.Join(tmpDir, "drm.xml")
	if err := os.WriteFile(drmPath, []byte(drmXML), 0600); err != nil {
		fatal("Failed to write drm.xml:", err)
	}

	encrypted := filepath.Join(tmpDir, "encrypted.mp4")
	run("MP4Box", "-quiet", "-crypt", drmPath, remuxed, "-out", encrypted)

	// 3. MSE-совместимая фрагментация: DASH onDemand — один самодостаточный
	//    файл (ftyp+moov+sidx+moof...), tfhd с default-base-is-moof
	run("MP4Box", "-quiet",
		"-dash", "4000",
		"-rap",
		"-profile", "onDemand",
		"-segment-name", "",
		"-out", filepath.Join(tmpDir, "manifest.mpd"),
		encrypted,
	)

	// DASH onDemand кладёт единственный сегмент-файл рядом с манифестом
	fragmented := ""
	entries, _ := os.ReadDir(tmpDir)
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".mp4") && name != "remuxed.mp4" && name != "encrypted.mp4" {
			fragmented = filepath.Join(tmpDir, name)
		}
	}
	if fragmented == "" {
		fatal("MP4Box did not produce a DASH segment file")
	}

	data, err := os.ReadFile(fragmented)
	if err != nil {
		fatal("Failed to read result:", err)
	}
	if err := os.WriteFile(output, data, 0644); err != nil {
		fatal("Failed to write output:", err)
	}

	// Метаданные для плеера (сервер читает при старте)
	metaPath := output + ".json"
	meta, _ := json.MarshalIndent(map[string]string{"mime": mime}, "", "  ")
	if err := os.WriteFile(metaPath, meta, 0644); err != nil {
		fatal("Failed to write meta:", err)
	}

	fmt.Println()
	fmt.Println("Done:", output)
	fmt.Println("Meta:", metaPath)
	fmt.Println()
	fmt.Println("Add to your .env:")
	fmt.Println("  CENC_KEY=" + key)
	fmt.Println("  CENC_KID=" + kid)
}
