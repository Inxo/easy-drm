package main

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"github.com/joho/godotenv"
	"github.com/rs/cors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

var key = []byte("") // Замените на свой секретный ключ
var wd = ""

func main() {
	var err error
	wd, err = os.Getwd()
	if err != nil {
		log.Fatal("Cannot get working directory")
	}
	err = godotenv.Load(wd + "/.env")
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	mux := http.NewServeMux()
	hostsStr := os.Getenv("CORS_HOSTS")
	key = []byte(os.Getenv("SECRET"))
	hosts := strings.Split(hostsStr, ",")
	crs := cors.New(cors.Options{
		AllowedOrigins:   hosts,
		AllowCredentials: false,
		AllowedHeaders:   []string{"Authorization", "Sec-Fetch-Mode", "Sec-Fetch-Dest", "Sec-Fetch-Site", "Content-Type", "Origin", "Accept", "Access-Control-Allow-Credentials"},
		// Enable Debugging for testing, consider disabling in production
		Debug: true,
	})
	handler := crs.Handler(mux)
	mux.HandleFunc("/stream", videoStreamHandler)
	srv := &http.Server{Addr: ":8080", Handler: handler}
	println("starting web: http://localhost:" + srv.Addr)
	err = srv.ListenAndServe()
	if err != nil {
		println("server not started: " + err.Error())
	}
}

func videoStreamHandler(w http.ResponseWriter, r *http.Request) {
	keyUrl := r.URL.Query().Get("key")
	fmt.Println("key: " + keyUrl)

	fmt.Println("wd: " + wd)
	key = []byte(keyUrl)
	videoFile, err := os.Open(wd + "/data/input.mp4") // Замените на путь к вашему исходному видеофайлу
	if err != nil {
		http.Error(w, "Unable to open video file", http.StatusInternalServerError)
		return
	}
	defer func(videoFile *os.File) {
		err := videoFile.Close()
		if err != nil {

		}
	}(videoFile)

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Transfer-Encoding", "chunked")

	block, err := aes.NewCipher(key)
	if err != nil {
		http.Error(w, "Error creating AES cipher", http.StatusInternalServerError)
		return
	}

	stream := cipher.NewCTR(block, make([]byte, aes.BlockSize))
	writer := &cipher.StreamWriter{S: stream, W: w}

	buf := make([]byte, 1024*aes.BlockSize)
	for {
		n, err := videoFile.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "Error reading video file", http.StatusInternalServerError)
			return
		}

		_, err = writer.Write(buf[:n])
		if err != nil {
			http.Error(w, "Error writing encrypted data to response", http.StatusInternalServerError)
			return
		}
	}

	fmt.Println("Video streaming completed.")
}
