package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"os"
)

// generateKey генерирует случайный ключ заданной длины
func generateKey(keyLength int) ([]byte, error) {
	key := make([]byte, keyLength)
	_, err := rand.Read(key)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// encrypt используется для шифрования данных с использованием AES
// encrypt используется для шифрования данных с использованием AES
func encrypt(data []byte, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	// Генерация IV (вектор инициализации)
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}

	// Создание шифровщика с использованием блочного режима CBC
	mode := cipher.NewCBCEncrypter(block, iv)

	// Дополнение данных до размера блока
	padLen := aes.BlockSize - (len(data) % aes.BlockSize)
	pad := bytes.Repeat([]byte{byte(padLen)}, padLen)
	data = append(data, pad...)

	// Добавление IV к зашифрованным данным
	encrypted := make([]byte, len(data)+aes.BlockSize)
	copy(encrypted[:aes.BlockSize], iv)

	// Шифрование данных
	mode.CryptBlocks(encrypted[aes.BlockSize:], data)

	return encrypted, nil
}

// writeToFile записывает данные в файл
func writeToFile(data []byte, filename string) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)

	_, err = file.Write(data)
	if err != nil {
		return err
	}

	fmt.Printf("Шифрование завершено. Зашифрованные данные сохранены в %s\n", filename)
	return nil
}

func main() {

	if len(os.Args) < 3 {
		fmt.Println("Ошибка параметров: input key output")
		return
	}
	// Загрузка видео из файла MP4
	inputFilename := os.Args[1]
	videoData, err := os.ReadFile(inputFilename)
	if err != nil {
		fmt.Println("Ошибка при чтении файла:", err)
		return
	}

	// Генерация случайного ключа для шифрования
	keyLength := 32 // 256 бит для AES-256
	key, err := generateKey(keyLength)
	if err != nil {
		fmt.Println("Ошибка при генерации ключа:", err)
		return
	}
	keyArgs := os.Args[2]
	key = []byte(keyArgs)
	fmt.Println("Key: " + string(key))

	// Шифрование видео
	encryptedVideo, err := encrypt(videoData, key)
	if err != nil {
		fmt.Println("Ошибка при шифровании видео:", err)
		return
	}

	// Сохранение зашифрованного видео в файл
	outputFilename := "encrypted_output.mp4"
	if len(os.Args) > 3 {
		outputFilename = os.Args[3]
	}
	err = writeToFile(encryptedVideo, outputFilename)
	if err != nil {
		fmt.Println("Ошибка при сохранении файла:", err)
		return
	}
}
