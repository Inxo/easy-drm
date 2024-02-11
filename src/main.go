package main

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"net/http"
	"os"
)

var aesKey = []byte("")

func main() {
	http.HandleFunc("/video", func(w http.ResponseWriter, r *http.Request) {
		aesKey = []byte(r.URL.Query().Get("key"))
		// Открытие файла
		file, err := os.Open("input.mp4")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Создание блочного шифра
		block, err := aes.NewCipher(aesKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Создание CTR режима шифрования
		stream := cipher.NewCTR(block, aesKey[:block.BlockSize()])

		// Отправка заголовков
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\"video.mp4\"")

		// Создание шифрованного потока записи
		writer := &cipher.StreamWriter{S: stream, W: w}

		// Чтение данных файла по блокам, их шифрование и запись в ответ
		buffer := make([]byte, 1024*1024) // Буфер размером 8KB
		fmt.Println("handle: " + string(aesKey))
		for {
			n, err := file.Read(buffer)
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if _, err := writer.Write(buffer[:n]); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
	})

	// Middleware для обработки CORS
	corsMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == "OPTIONS" {
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	err := http.ListenAndServe(":8080", corsMiddleware(http.DefaultServeMux))
	if err != nil {
		return
	}
}
