package internal

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

type Encryptor struct {
	secret     string // signature encoded with Base64
	parameters string
	order      string // order number to be encrypted
}

func NewEncryptor(secret string, parameters string, order string) *Encryptor {
	return &Encryptor{
		secret:     secret,
		parameters: parameters,
		order:      order,
	}
}

func (e *Encryptor) CreateSignature() (string, error) {

	key, err := base64.StdEncoding.DecodeString(e.secret)
	if err != nil {
		return "", fmt.Errorf("decode secret: %v", err)
	}

	// encrypt signature with 3DES
	signatureEncrypted, err := e.encrypt3DES(e.order, key)
	if err != nil {
		return "", fmt.Errorf("encrypt3DES: %v", err)
	}

	// create hash with SHA256
	hash := e.mac256(e.parameters, signatureEncrypted)
	// encode hash to Base64
	signature := base64.StdEncoding.EncodeToString(hash)

	return signature, nil
}

func (e *Encryptor) encrypt3DES(plainText string, key []byte) ([]byte, error) {
	if plainText == "" {
		return nil, errors.New("plainText cannot be empty")
	}

	toEncryptArray := []byte(plainText)

	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return nil, err
	}

	// SALT used in 3DES encryption process
	salt := []byte{0, 0, 0, 0, 0, 0, 0, 0}

	// Padding
	padding := block.BlockSize() - len(toEncryptArray)%block.BlockSize()
	addText := bytes.Repeat([]byte{0}, padding)
	toEncryptArray = append(toEncryptArray, addText...)

	ciphertext := make([]byte, len(toEncryptArray))

	// Create the CBC mode
	mode := cipher.NewCBCEncrypter(block, salt)

	// Encrypt
	//fmt.Printf("Key: %x\n", key)
	//fmt.Printf("Cleartext: %x\n", toEncryptArray)
	mode.CryptBlocks(ciphertext, toEncryptArray)
	//fmt.Printf("Ciphertext: %x\n", ciphertext)

	return ciphertext, nil
}

func (e *Encryptor) mac256(message string, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}
