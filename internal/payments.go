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
	"strconv"
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
	if amount <= 0 || !transaction.IsFinished {
		p.logger.Warn(fmt.Sprintf("transaction %v is not finished or amount is zero", transactionId))
		return nil
	}

	tag, err := p.database.GetUserTag(transaction.IdTag)
	if err != nil {
		p.logger.Error("failed to get user tag", err)
		return err
	}
	if tag.UserId == "" {
		//p.logger.Warn(fmt.Sprintf("empty user id for tag %v", tag.IdTag))

		transaction.PaymentBilled = transaction.PaymentAmount
		err = p.database.UpdateTransaction(transaction)
		if err != nil {
			p.logger.Error("update transaction", err)
		}

		return fmt.Errorf("empty user id for tag %v", transaction.IdTag)
	}
	paymentMethod, err := p.database.GetPaymentMethod(tag.UserId)
	if err != nil {
		//p.logger.Error("failed to get payment method", err)

		transaction.PaymentBilled = transaction.PaymentAmount
		err = p.database.UpdateTransaction(transaction)
		if err != nil {
			p.logger.Error("update transaction", err)
		}

		return fmt.Errorf("id %v has no payment method", transaction.IdTag)
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
		p.logger.Error("create parameters", err)
		return err
	}

	encryptor := NewEncryptor(secret, parametersBase64, order)
	signature, err := encryptor.CreateSignature()
	if err != nil {
		p.logger.Error("create signature", err)
		return err
	}

	request := &models.PaymentRequest{
		Parameters:       parametersBase64,
		Signature:        signature,
		SignatureVersion: "HMAC_SHA256_V1",
	}

	go p.processRequest(request)

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

func (p *Payments) processRequest(request *models.PaymentRequest) {
	requestData, err := json.Marshal(request)
	if err != nil {
		p.logger.Error("create request", err)
		return
	}

	apiUrl := testUrl
	if !p.conf.Merchant.Test {
		apiUrl = prodUrl
	}
	response, err := http.Post(apiUrl, "application/json", bytes.NewBuffer(requestData))
	if err != nil {
		p.logger.Error("post request", err)
		return
	}
	paymentResult, err := p.readResponse(response)
	if err != nil {
		p.logger.Error("read response", err)
		return
	}

	p.processResponse(paymentResult)

}

func (p *Payments) readResponse(response *http.Response) (*models.PaymentParameters, error) {
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %v", err)
	}
	var paymentResponse models.PaymentRequest
	err = json.Unmarshal(body, &paymentResponse)
	if err != nil {
		return nil, fmt.Errorf("parse response: %v", err)
	}

	parameters, err := base64.StdEncoding.DecodeString(paymentResponse.Parameters)
	if err != nil {
		return nil, fmt.Errorf("decode parameters: %v", err)
	}
	var paymentResult models.PaymentParameters
	err = json.Unmarshal(parameters, &paymentResult)
	if err != nil {
		return nil, fmt.Errorf("parse parameters: %v", err)
	}

	return &paymentResult, nil
}

func (p *Payments) processResponse(paymentResult *models.PaymentParameters) {

	err := p.database.SavePaymentResult(paymentResult)
	if err != nil {
		p.logger.Error("save payment result", err)
	}

	number, err := strconv.Atoi(paymentResult.Order)
	if err != nil {
		p.logger.Error("read order number", err)
		return
	}
	amount, err := strconv.Atoi(paymentResult.Amount)
	if err != nil {
		p.logger.Error("read amount", err)
		return
	}
	order, err := p.database.GetPaymentOrder(number)
	if err != nil {
		p.logger.Error("get payment order", err)
		return
	}
	if !order.IsCompleted {
		order.Amount = amount
		order.IsCompleted = true
		order.Result = fmt.Sprintf("%s by electrum", paymentResult.Response)
		order.TimeClosed = time.Now()
		order.Currency = paymentResult.Currency
		order.Date = fmt.Sprintf("%s %s", paymentResult.Date, paymentResult.Hour)

		err = p.database.SavePaymentOrder(order)
		if err != nil {
			p.logger.Error("save payment order", err)
		}
	}

	if paymentResult.Response != "0000" {
		p.logger.Warn(fmt.Sprintf("error %s; transaction %v; order %s; amount %s", paymentResult.Response, order.TransactionId, paymentResult.Order, paymentResult.Amount))
		return
	}
	p.logger.Info(fmt.Sprintf("transaction %v; order %s accepted; amount %s", order.TransactionId, paymentResult.Order, paymentResult.Amount))

	if order.TransactionId > 0 {

		transaction, err := p.database.GetTransaction(order.TransactionId)
		if err != nil {
			p.logger.Error("get transaction", err)
			return
		}
		if transaction.PaymentOrder == 0 {
			transaction.PaymentOrder = order.Order
			transaction.PaymentBilled = order.Amount

			err = p.database.UpdateTransaction(transaction)
			if err != nil {
				p.logger.Error("update transaction", err)
				return
			}
		}

		//} else {
		//
		//	paymentMethod := models.PaymentMethod{
		//		Description: "**** **** **** ****",
		//		Identifier:  params.MerchantIdentifier,
		//		CardBrand:   params.CardBrand,
		//		CardCountry: params.CardCountry,
		//		ExpiryDate:  params.ExpiryDate,
		//		UserId:      order.UserId,
		//		UserName:    order.UserName,
		//	}
		//	err = p.database.SavePaymentMethod(&paymentMethod)
		//	if err != nil {
		//		p.logger.Error("save payment method", err)
		//		return
		//	}

	}

}
