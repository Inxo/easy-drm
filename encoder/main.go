package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// EDRM format:
//   [4]  magic "EDRM"
//   [4]  version = 1  (uint32 LE)
//   [4]  chunk count  (uint32 LE)
//   [4]  plain chunk size in bytes (uint32 LE)
//   per chunk:
//     [12] GCM nonce
//     [4]  encrypted length (uint32 LE)
//     [N]  AES-GCM ciphertext  (plain + 16-byte tag)

const plainChunkSize = 512 * 1024 // 512 KB

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "Usage: encoder <input.mp4> <output.enc> <hex_aes256_key>")
		fmt.Fprintln(os.Stderr, "  Generate key: openssl rand -hex 32")
		os.Exit(1)
	}

	inputPath := os.Args[1]
	outputPath := os.Args[2]
	keyHex := os.Args[3]

	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		fmt.Fprintln(os.Stderr, "Key must be 64 hex chars (32 bytes / AES-256)")
		os.Exit(1)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to create cipher:", err)
		os.Exit(1)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to create GCM:", err)
		os.Exit(1)
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to read input:", err)
		os.Exit(1)
	}

	chunkCount := (len(data) + plainChunkSize - 1) / plainChunkSize

	out, err := os.Create(outputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Failed to create output:", err)
		os.Exit(1)
	}
	defer out.Close()

	// Write header
	out.Write([]byte("EDRM"))
	binary.Write(out, binary.LittleEndian, uint32(1))
	binary.Write(out, binary.LittleEndian, uint32(chunkCount))
	binary.Write(out, binary.LittleEndian, uint32(plainChunkSize))

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes

	for i := 0; i < chunkCount; i++ {
		start := i * plainChunkSize
		end := start + plainChunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[start:end]

		if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
			fmt.Fprintln(os.Stderr, "Failed to generate nonce:", err)
			os.Exit(1)
		}

		enc := gcm.Seal(nil, nonce, chunk, nil)

		out.Write(nonce)
		binary.Write(out, binary.LittleEndian, uint32(len(enc)))
		out.Write(enc)

		fmt.Printf("chunk %d/%d  (%d bytes)\n", i+1, chunkCount, len(enc))
	}

	fmt.Printf("\nDone: %s → %s  (%d chunks)\n", inputPath, outputPath, chunkCount)
}
