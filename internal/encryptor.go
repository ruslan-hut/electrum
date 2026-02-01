// Package internal provides core payment processing services for the Electrum
// payment gateway integration with Redsys.
// https://pagosonline.redsys.es/desarrolladores-inicio/documentacion-operativa/autorizacion/
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

// Encryptor handles cryptographic signature generation for Redsys payment gateway.
// It implements the Redsys-specific signature algorithm using 3DES encryption
// and HMAC-SHA256 for request authentication.
type Encryptor struct {
	secret     string // merchant secret key encoded with Base64
	parameters string // Base64-encoded merchant parameters
	order      string // order number to be encrypted
}

// NewEncryptor creates a new Encryptor for Redsys signature generation.
// The secret must be Base64-encoded, as provided by Redsys.
func NewEncryptor(secret string, parameters string, order string) *Encryptor {
	return &Encryptor{
		secret:     secret,
		parameters: parameters,
		order:      order,
	}
}

// CreateSignature generates a Redsys-compliant signature using 3DES and HMAC-SHA256.
// The signature process:
// 1. Decode the merchant secret from Base64
// 2. Encrypt the order number using 3DES-CBC with zero-padding (Redsys requirement)
// 3. Use the encrypted result as HMAC key to sign the parameters
// 4. Return the Base64-encoded HMAC signature
func (e *Encryptor) CreateSignature() (string, error) {
	key, err := base64.StdEncoding.DecodeString(e.secret)
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}

	// Encrypt order number with 3DES using Redsys-specific algorithm
	signatureEncrypted, err := e.encrypt3DES(e.order, key)
	if err != nil {
		return "", fmt.Errorf("encrypt3DES: %w", err)
	}

	// Create HMAC-SHA256 signature using encrypted order as key
	hash := e.mac256(e.parameters, signatureEncrypted)

	// Encode signature to Base64 for transmission
	signature := base64.StdEncoding.EncodeToString(hash)

	return signature, nil
}

// encrypt3DES encrypts plaintext using 3DES in CBC mode with zero-padding.
// Redsys-specific requirements (mandated by their API specification):
// 1. Fixed all-zero IV (not cryptographically secure but required)
// 2. Zero-padding (NOT PKCS#7 - this is critical for signature verification)
// Security notes:
// - The encrypted output is used as an HMAC key, not for confidentiality
// - Zero-padding is safe here because order numbers are numeric strings
// - These non-standard practices are mandated by Redsys payment gateway
func (e *Encryptor) encrypt3DES(plainText string, key []byte) ([]byte, error) {
	if plainText == "" {
		return nil, errors.New("plainText cannot be empty")
	}

	toEncryptArray := []byte(plainText)

	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create 3DES cipher: %w", err)
	}

	// Fixed IV as required by Redsys specification
	// WARNING: This is not cryptographically secure but is mandated by Redsys
	iv := []byte{0, 0, 0, 0, 0, 0, 0, 0}

	// Apply zero-padding as required by Redsys signature algorithm
	// NOTE: Redsys expects zero-padding, NOT PKCS#7 padding
	// Zero-padding is safe here because order numbers are numeric strings
	toEncryptArray = zeroPad(toEncryptArray, block.BlockSize())

	ciphertext := make([]byte, len(toEncryptArray))

	// Encrypt using CBC mode with fixed IV
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, toEncryptArray)

	return ciphertext, nil
}

// zeroPad applies zero-byte padding to make data a multiple of blockSize.
// This is specifically required by Redsys for signature calculation.
// Zero-padding appends null bytes (0x00) until the data length is a multiple of blockSize.
func zeroPad(data []byte, blockSize int) []byte {
	padding := blockSize - (len(data) % blockSize)
	if padding == blockSize {
		// Already aligned, no padding needed
		return data
	}
	padText := bytes.Repeat([]byte{0x00}, padding)
	return append(data, padText...)
}

// mac256 computes HMAC-SHA256 of the message using the provided key.
// This is used to create the final signature for Redsys requests.
func (e *Encryptor) mac256(message string, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}
