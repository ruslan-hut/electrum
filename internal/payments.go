package internal

import (
	"bytes"
	"electrum/config"
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
	testUrl = "https://sis-t.redsys.es:25443/sis/rest/trataPeticionREST"
	prodUrl = "https://sis.redsys.es/sis/rest/trataPeticionREST"
)

type Payments struct {
	conf     *config.Config
	database services.Database
	logger   services.LogHandler
	mutex    *sync.Mutex
}

func NewPayments(config *config.Config) *Payments {
	return &Payments{
		conf:  config,
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
	p.Lock()
	defer p.Unlock()

	if p.database == nil {
		return fmt.Errorf("database not set")
	}
	if p.conf.Merchant.Secret == "" || p.conf.Merchant.Code == "" || p.conf.Merchant.Terminal == "" {
		return fmt.Errorf("merchant not configured")
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
	secret := p.conf.Merchant.Secret

	parameters := models.MerchantParameters{
		Amount:          fmt.Sprintf("%d", amount),
		Order:           order,
		Identifier:      paymentOrder.Identifier,
		MerchantCode:    p.conf.Merchant.Code,
		Currency:        "978",
		TransactionType: "0",
		Terminal:        p.conf.Merchant.Terminal,
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

	encryptor := NewEncryptor(secret, parametersBase64, order)
	signature, err := encryptor.CreateSignature()
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

	apiUrl := testUrl
	if !p.conf.Merchant.Test {
		apiUrl = prodUrl
	}
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
