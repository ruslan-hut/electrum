package internal

import (
	"bytes"
	"crypto/cipher"
	"crypto/des"
	"crypto/hmac"
	"crypto/sha256"
	"electrum/models"
	"electrum/services"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	// for tests: https://sis-t.redsys.es:25443/sis/rest/trataPeticionREST
	// production: https://sis.redsys.es/sis/rest/trataPeticionREST
	apiUrl = "https://sis-t.redsys.es:25443/sis/rest/trataPeticionREST"
)

type Payments struct {
	database services.Database
	logger   services.LogHandler
	mutex    *sync.Mutex
}

func NewPayments() *Payments {
	return &Payments{
		mutex: &sync.Mutex{},
	}
}

func (p *Payments) Lock() {
	p.mutex.Lock()
}

func (p *Payments) Unlock() {
	p.mutex.Unlock()
}

func (p *Payments) SetDatabase(database services.Database) {
	p.database = database
}

func (p *Payments) SetLogger(logger services.LogHandler) {
	p.logger = logger
}

func (p *Payments) PayTransaction(transactionId int) error {

	if p.database == nil {
		return fmt.Errorf("database not set")
	}

	transaction, err := p.database.GetTransaction(transactionId)
	if err != nil {
		p.logger.Error(fmt.Sprintf("failed to get transaction %v", transactionId), err)
		return err
	}

	amount := transaction.PaymentAmount - transaction.PaymentBilled
	if amount <= 0 || !transaction.IsFinished || transaction.Username == "" {
		p.logger.Warn(fmt.Sprintf("transaction %v is not finished or amount is zero", transactionId))
		return nil
	}

	tag, err := p.database.GetUserTag(transaction.IdTag)
	if err != nil {
		p.logger.Error("failed to get user tag", err)
		return err
	}
	if tag.UserId == "" {
		p.logger.Warn(fmt.Sprintf("empty user id for tag %v", tag.IdTag))
		return fmt.Errorf("empty user id")
	}
	paymentMethod, err := p.database.GetPaymentMethod(tag.UserId)
	if err != nil {
		p.logger.Error("failed to get payment method", err)
		return err
	}
	consumed := (transaction.MeterStop - transaction.MeterStart) / 1000
	description := fmt.Sprintf("%s:%d %dkW", transaction.ChargePointId, transaction.ConnectorId, consumed)

	orderToClose, _ := p.database.GetPaymentOrderByTransaction(transaction.Id)
	if orderToClose != nil {
		orderToClose.IsCompleted = true
		orderToClose.Result = "closed without response"
		orderToClose.TimeClosed = time.Now()
		_ = p.database.SavePaymentOrder(orderToClose)
	}

	paymentOrder := models.PaymentOrder{
		Amount:        amount,
		Description:   description,
		Identifier:    paymentMethod.Identifier,
		TransactionId: transaction.Id,
		UserId:        tag.UserId,
		UserName:      tag.Username,
		TimeOpened:    time.Now(),
	}

	lastOrder, _ := p.database.GetLastOrder()
	if lastOrder != nil {
		paymentOrder.Order = lastOrder.Order + 1
	} else {
		paymentOrder.Order = 1200
	}

	err = p.database.SavePaymentOrder(&paymentOrder)
	if err != nil {
		p.logger.Error("failed to save order", err)
		return err
	}

	order := fmt.Sprintf("%d", paymentOrder.Order)
	secret := "sq7HjrUOBfKmC576ILgskD5srU870gJ7"

	parameters := models.MerchantParameters{
		Amount:          fmt.Sprintf("%d", amount),
		Order:           order,
		Identifier:      paymentOrder.Identifier,
		MerchantCode:    "358333276",
		Currency:        "978",
		TransactionType: "0",
		Terminal:        "001",
		DirectPayment:   "true",
		Exception:       "MIT",
		Cof:             "N",
	}

	// encode parameters to Base64
	parametersBase64, err := p.createParameters(&parameters)
	if err != nil {
		p.logger.Error("failed to create parameters", err)
		return err
	}
	signature, err := p.createSignature(order, parametersBase64, secret)
	if err != nil {
		p.logger.Error("failed to create signature", err)
		return err
	}

	request := models.PaymentRequest{
		Parameters:       parametersBase64,
		Signature:        signature,
		SignatureVersion: "HMAC_SHA256_V1",
	}
	requestData, err := json.Marshal(request)
	if err != nil {
		p.logger.Error("failed to create request", err)
		return err
	}
	p.logger.Info(fmt.Sprintf("request: %s", string(requestData)))
	//decodedParameters, err := base64.StdEncoding.DecodeString(parametersBase64)
	//if err != nil {
	//	return err
	//}
	//p.logger.Info(fmt.Sprintf("parameters: %s", decodedParameters))

	response, err := http.Post(apiUrl, "application/json", bytes.NewBuffer(requestData))
	if err != nil {
		p.logger.Error("failed to send request", err)
		return err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			p.logger.Error("failed to close response body", err)
		}
	}(response.Body)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		p.logger.Error("failed to read response body", err)
		return err
	}

	//var paymentResponse models.PaymentRequest
	//err = json.Unmarshal(body, &paymentResponse)
	//if err != nil {
	//	p.logger.Warn(fmt.Sprintf("response: %s", string(body)))
	//	p.logger.Error("failed to parse response", err)
	//	return err
	//}
	p.logger.Info(fmt.Sprintf("response: %s", string(body)))

	return nil
}

func (p *Payments) createParameters(parameters *models.MerchantParameters) (string, error) {
	// convert parameters to JSON string
	parametersJson, err := json.Marshal(parameters)
	if err != nil {
		return "", err
	}
	// encode parameters to Base64
	return base64.StdEncoding.EncodeToString(parametersJson), nil
}

func (p *Payments) createSignature(order, parameters, secret string) (string, error) {

	key, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return "", fmt.Errorf("decode secret: %v", err)
	}

	// encrypt signature with 3DES
	signatureEncrypted, err := encrypt3DES([]byte(order), key)
	if err != nil {
		return "", err
	}
	// create hash with SHA256
	hash := mac256(parameters, signatureEncrypted)
	// encode hash to Base64
	return base64.StdEncoding.EncodeToString(hash), nil
}

func mac256(message string, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(message))
	return mac.Sum(nil)
}

func encrypt3DES(message, key []byte) ([]byte, error) {
	block, err := des.NewTripleDESCipher(key)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, 8) // 8 bytes for DES and 3DES

	// Pad the message to be a multiple of the block size
	padding := 8 - len(message)%8
	if padding == 8 {
		padding = 0
	}
	padText := bytes.Repeat([]byte("0"), padding)
	message = append(message, padText...)

	ciphertext := make([]byte, len(message))

	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(ciphertext, message)

	return ciphertext, nil
}
