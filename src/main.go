package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

var aesKey = []byte("your_secret_aes_key_32_charslong")

func encrypt(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, data, nil)
	ciphertext = append(nonce, ciphertext...)
	return ciphertext, nil
}

var wd = ""

func main() {
	wd, err := os.Getwd()
	if err != nil {
		log.Fatal("Cannot get working directory")
	}
	fmt.Println("wd: " + wd)
	http.HandleFunc("/video", func(w http.ResponseWriter, r *http.Request) {

		// Чтение mp4-файла
		file, err := os.Open(wd + "/data/input.mp4")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer file.Close()

		videoData, err := ioutil.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		encryptedData, err := encrypt(videoData, aesKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(encryptedData)
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

	http.ListenAndServe(":8080", corsMiddleware(http.DefaultServeMux))
}
