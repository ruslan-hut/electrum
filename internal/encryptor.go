package internal

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"gitee.com/golang-module/dongle"
	"strconv"
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
	keyHex := e.toHexadecimal(key, 24)
	keyBytes := e.toByteArray(keyHex)

	// encrypt signature with 3DES
	signatureEncrypted := e.encrypt3DES([]byte(e.order), keyBytes)

	// create hash with SHA256
	hash := e.mac256(e.parameters, signatureEncrypted)
	// encode hash to Base64
	return base64.StdEncoding.EncodeToString(hash), nil
}

func (e *Encryptor) encrypt3DES(key, data []byte) []byte {

	padding := 8 - len(data)%8
	if padding == 8 {
		padding = 0
	}
	padText := bytes.Repeat([]byte("0"), padding)
	message := append(data, padText...)

	cipher := dongle.NewCipher()
	cipher.SetMode(dongle.CBC)   // CBC、ECB、CFB、OFB、CTR
	cipher.SetPadding(dongle.No) // No、Empty、Zero、PKCS5、PKCS7、AnsiX923、ISO97971
	cipher.SetKey(key)           // key must be 24 bytes
	cipher.SetIV("00000000")     // iv must be 8 bytes
	return dongle.Encrypt.FromBytes(message).By3Des(cipher).ToRawBytes()
}

func (e *Encryptor) mac256(message string, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

func (e *Encryptor) toHexadecimal(data []byte, numBytes int) string {
	var result string
	input := bytes.NewBuffer(data[:numBytes])

	for {
		sym, err := input.ReadByte()
		if err != nil {
			break
		}
		cadAux := fmt.Sprintf("%x", sym)
		if len(cadAux) < 2 {
			result += "0"
		}
		result += cadAux
	}

	return result
}

func (e *Encryptor) toByteArray(chain string) []byte {
	if len(chain)%2 != 0 {
		chain = "0" + chain
	}

	length := len(chain) / 2
	position := 0
	var chainAux string
	result := new(bytes.Buffer)

	for i := 0; i < length; i++ {
		chainAux = chain[position : position+2]
		position += 2
		val, _ := strconv.ParseInt(chainAux, 16, 8)
		result.WriteByte(byte(val))
	}

	return result.Bytes()
}

//func encrypt3DES(message, key []byte) ([]byte, error) {
//	block, err := des.NewTripleDESCipher(key)
//	if err != nil {
//		return nil, err
//	}
//	iv := make([]byte, 8) // 8 bytes for DES and 3DES
//
//	// Pad the message to be a multiple of the block size
//	padding := 8 - len(message)%8
//	if padding == 8 {
//		padding = 0
//	}
//	padText := bytes.Repeat([]byte("0"), padding)
//	message = append(message, padText...)
//
//	ciphertext := make([]byte, len(message))
//
//	mode := cipher.NewCBCEncrypter(block, iv)
//	mode.CryptBlocks(ciphertext, message)
//
//	return ciphertext, nil
//}
