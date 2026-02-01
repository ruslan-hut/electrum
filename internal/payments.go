package internal

import (
	"bytes"
	"context"
	"electrum/config"
	"electrum/entity"
	"electrum/services"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Payments handles payment processing with Redsys payment gateway.
// It uses fine-grained locking per transaction/order to allow concurrent operations
// while preventing race conditions.
type Payments struct {
	conf       *config.Config
	database   services.Database
	logger     services.LogHandler
	locks      sync.Map // map[int]*sync.Mutex for per-order locking
	requestUrl string
	httpClient *http.Client
}

// NewPayments creates a new payment processing service with configured HTTP client.
// The HTTP client includes timeouts and connection pooling for reliable external API calls.
func NewPayments(config *config.Config) *Payments {
	return &Payments{
		conf:       config,
		requestUrl: config.Merchant.RequestUrl,
		locks:      sync.Map{},
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		},
	}
}

// lockOrder acquires a lock for a specific transaction/order to prevent concurrent modifications.
// This allows multiple different orders to be processed in parallel while ensuring
// safety for operations on the same order.
func (p *Payments) lockOrder(id int) *sync.Mutex {
	value, _ := p.locks.LoadOrStore(id, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex
}

// unlockOrder releases the lock for a transaction/order and cleans up the mutex
// from the map to prevent memory leaks.
func (p *Payments) unlockOrder(id int, mutex *sync.Mutex) {
	mutex.Unlock()
	// Clean up mutex from map to prevent unbounded growth
	p.locks.Delete(id)
}

func (p *Payments) SetDatabase(database services.Database) {
	p.database = database
}

func (p *Payments) SetLogger(logger services.LogHandler) {
	p.logger = logger
	if p.conf.DisablePayment {
		p.logger.Warn("service disabled")
	} else {
		p.logger.Info("service enabled")
	}
}

// Notify processes a payment notification webhook from Redsys.
// Note: Notify doesn't lock because it processes asynchronously and doesn't
// directly modify shared state - the async processResponse handles its own locking.
func (p *Payments) Notify(ctx context.Context, data []byte) error {

	params, err := url.ParseQuery(string(data))
	if err != nil {
		p.logger.Info(string(data))
		return fmt.Errorf("parse query: %v", err)
	}

	paymentResult := entity.PaymentRequest{
		SignatureVersion: params.Get("Ds_SignatureVersion"),
		Parameters:       params.Get("Ds_MerchantParameters"),
		Signature:        params.Get("Ds_Signature"),
	}

	response, err := p.readParameters(paymentResult.Parameters)
	if response != nil {
		// Process payment response asynchronously with panic recovery
		go p.processResponseWithRecovery(ctx, response)
	}
	return err
}

// PayTransaction initiates a payment for a finished charging transaction.
// Uses per-transaction locking to allow concurrent payments for different transactions.
func (p *Payments) PayTransaction(ctx context.Context, transactionId int) error {
	mutex := p.lockOrder(transactionId)
	defer p.unlockOrder(transactionId, mutex)

	p.logger.Info(fmt.Sprintf("pay transaction %v", transactionId))

	if p.conf.Merchant.Secret == "" || p.conf.Merchant.Code == "" || p.conf.Merchant.Terminal == "" {
		return fmt.Errorf("merchant not configured")
	}

	transaction, err := p.getTransaction(ctx, transactionId)
	if err != nil {
		p.logger.Error(fmt.Sprintf("pay transaction %v", transactionId), err)
		return err
	}
	amount := transaction.PaymentAmount - transaction.PaymentBilled
	if amount <= 0 {
		p.logger.Warn(fmt.Sprintf("transaction %v amount is zero", transactionId))
		return nil
	}

	// --------------------------------------------- USER TAG
	tag := transaction.UserTag
	if tag == nil {
		tag, err = p.database.GetUserTag(ctx, transaction.IdTag)
		if err != nil {
			p.logger.Error("get user tag", err)
			return err
		}
	}
	if tag.UserId == "" {
		//p.logger.Warn(fmt.Sprintf("empty user id for tag %v", tag.IdTag))

		transaction.PaymentBilled = transaction.PaymentAmount
		err = p.database.UpdateTransaction(ctx, transaction)
		if err != nil {
			p.logger.Error("update transaction", err)
		}

		return fmt.Errorf("empty user id for tag %v", secret(transaction.IdTag))
	}

	// --------------------------------------------- PAYMENT METHOD
	paymentMethod := transaction.PaymentMethod
	if paymentMethod == nil {
		paymentMethod, err = p.database.GetPaymentMethod(ctx, tag.UserId)
		if err != nil {
			//p.logger.Error("failed to get payment method", err)

			transaction.PaymentBilled = transaction.PaymentAmount
			err = p.database.UpdateTransaction(ctx, transaction)
			if err != nil {
				p.logger.Error("update transaction", err)
			}

			return fmt.Errorf("id %v has no payment method", secret(transaction.IdTag))
		}
	}
	// try to get another payment method if the current has some problems or the transaction has previous errors
	if paymentMethod.CofTid == "" || paymentMethod.FailCount > 0 || transaction.PaymentError != "" {
		storedPM, _ := p.database.GetPaymentMethod(ctx, tag.UserId)
		if storedPM != nil && storedPM.Identifier != paymentMethod.Identifier {
			paymentMethod = storedPM
			p.logger.Warn(fmt.Sprintf("payment method loaded from db: %s", secret(storedPM.Identifier)))
		}
	}

	consumed := (transaction.MeterStop - transaction.MeterStart) / 1000
	description := fmt.Sprintf("%s:%d %dkW", transaction.ChargePointId, transaction.ConnectorId, consumed)

	orderToClose, err := p.database.GetPaymentOrderByTransaction(ctx, transaction.Id)
	if err == nil && orderToClose != nil {
		orderToClose.IsCompleted = true
		orderToClose.Result = "closed without response"
		orderToClose.TimeClosed = time.Now()
		if err := p.database.SavePaymentOrder(ctx, orderToClose); err != nil {
			p.logger.Error("failed to close previous payment order", err)
		}
		p.updatePaymentMethodFailCounter(ctx, orderToClose.Identifier, 1)
	}

	//---------------------------------------------
	if p.conf.DisablePayment {
		transaction.PaymentBilled = transaction.PaymentAmount
		err = p.database.UpdateTransaction(ctx, transaction)
		if err != nil {
			p.logger.Error("update transaction", err)
		}
		p.logger.Info(fmt.Sprintf("payment disabled: transaction %v paid without request", transactionId))
		return nil
	}
	//---------------------------------------------

	paymentOrder := entity.PaymentOrder{
		Amount:        amount,
		Description:   description,
		Identifier:    paymentMethod.Identifier,
		TransactionId: transaction.Id,
		UserId:        tag.UserId,
		UserName:      tag.Username,
		TimeOpened:    time.Now(),
	}

	lastOrder, _ := p.database.GetLastOrder(ctx)
	if lastOrder != nil {
		paymentOrder.Order = lastOrder.Order + 1
	} else {
		paymentOrder.Order = 1200
	}

	err = p.database.SavePaymentOrder(ctx, &paymentOrder)
	if err != nil {
		p.logger.Error("save order", err)
		return err
	}

	order := fmt.Sprintf("%d", paymentOrder.Order)

	// Prepare Redsys MIT (Merchant Initiated Transaction) parameters
	// This is a subsequent recurring payment using stored credentials
	parameters := entity.MerchantParameters{
		Amount:          fmt.Sprintf("%d", amount),
		Order:           order,
		Identifier:      paymentMethod.Identifier,
		MerchantCode:    p.conf.Merchant.Code,
		Currency:        "978", // EUR
		TransactionType: "0",   // 0 = Authorization/Purchase, 3 = Refund
		Terminal:        p.conf.Merchant.Terminal,
		// DirectPayment: "true" for MIT using stored token (no redirect)
		DirectPayment: "true",
		// Exception: "MIT" signals PSD2 Merchant Initiated Transaction exemption
		// Required for merchant-initiated payments without cardholder participation
		Exception: "MIT",
		// CofIni: "N" indicates this is NOT the initial credential storage transaction
		// Initial transactions use "S", subsequent use "N"
		CofIni: "N",
		// CofType: "R" for Recurring payments (variable amounts, defined intervals)
		// "R" = Recurring (EV charging sessions with variable amounts)
		// "I" = Installments (fixed amounts, fixed intervals)
		// "C" = Others (one-time misc transactions)
		CofType: "R",
		// CofTid: Network transaction ID from the initial authorization
		// This links the current MIT transaction to the original cardholder-initiated auth
		CofTid: paymentMethod.CofTid,
	}
	p.logger.Info(fmt.Sprintf("order: %s; identifier: %s; txnid: %s", order, secret(parameters.Identifier), secret(parameters.CofTid)))

	request, err := p.newRequest(&parameters)
	if err != nil {
		p.logger.Error("pay: create request", err)
		return err
	}

	// Process payment request asynchronously with timeout
	go p.processRequestWithTimeout(ctx, request, paymentOrder.Order)

	return nil
}

// ReturnPayment processes a refund for a charging transaction.
// Uses per-transaction locking to allow concurrent operations.
func (p *Payments) ReturnPayment(ctx context.Context, transactionId int) error {
	mutex := p.lockOrder(transactionId)
	defer p.unlockOrder(transactionId, mutex)

	transaction, err := p.getTransaction(ctx, transactionId)
	if err != nil {
		p.logger.Error(fmt.Sprintf("return transaction %v", transactionId), err)
		return err
	}
	amount := transaction.PaymentAmount
	if amount <= 0 {
		p.logger.Warn(fmt.Sprintf("transaction %v amount is zero", transactionId))
		return nil
	}
	order := fmt.Sprintf("%d", transaction.PaymentOrder)

	parameters := entity.MerchantParameters{
		Amount: fmt.Sprintf("%d", amount),
		Order:  order,
		//Identifier:      paymentOrder.Identifier,
		MerchantCode:    p.conf.Merchant.Code,
		Currency:        "978",
		TransactionType: "3",
		Terminal:        p.conf.Merchant.Terminal,
		//DirectPayment:   "true",
		//Exception:       "MIT",
		//Cof:             "N",
	}

	request, err := p.newRequest(&parameters)
	if err != nil {
		p.logger.Error("return: create request", err)
		return err
	}

	// Process refund request asynchronously with timeout
	go p.processRequestWithTimeout(ctx, request, transaction.PaymentOrder)

	return nil
}

// ReturnByOrder processes a refund for a specific payment order.
// Uses per-order locking to allow concurrent refund operations.
func (p *Payments) ReturnByOrder(ctx context.Context, orderId string, amount int) error {
	if amount == 0 {
		return fmt.Errorf("amount to return is zero")
	}
	if p.database == nil {
		return fmt.Errorf("database not set")
	}
	id, err := strconv.Atoi(orderId)
	if err != nil {
		return fmt.Errorf("invalid order id: %s; %v", orderId, err)
	}

	mutex := p.lockOrder(id)
	defer p.unlockOrder(id, mutex)
	order, err := p.database.GetPaymentOrder(ctx, id)
	if err != nil {
		return fmt.Errorf("get payment order: %v", err)
	}
	if order.Amount < amount {
		return fmt.Errorf("order amount %v is less than return amount %v", order.Amount, amount)
	}

	parameters := entity.MerchantParameters{
		Amount:          fmt.Sprintf("%d", amount),
		Order:           orderId,
		MerchantCode:    p.conf.Merchant.Code,
		Currency:        "978",
		TransactionType: "3",
		Terminal:        p.conf.Merchant.Terminal,
	}

	request, err := p.newRequest(&parameters)
	if err != nil {
		p.logger.Error("return by order: create request", err)
		return err
	}

	// Process refund request asynchronously with timeout
	go p.processRequestWithTimeout(ctx, request, id)

	return nil
}

func (p *Payments) newRequest(parameters *entity.MerchantParameters) (*entity.PaymentRequest, error) {
	// encode parameters to Base64
	parametersBase64, err := p.createParameters(parameters)
	if err != nil {
		return nil, fmt.Errorf("parameters encode base64: %v", err)
	}

	order := parameters.Order
	merchantSecret := p.conf.Merchant.Secret

	encryptor := NewEncryptor(merchantSecret, parametersBase64, order)
	signature, err := encryptor.CreateSignature()
	if err != nil {
		return nil, fmt.Errorf("create signature: %v", err)
	}

	request := &entity.PaymentRequest{
		Parameters:       parametersBase64,
		Signature:        signature,
		SignatureVersion: "HMAC_SHA256_V1",
	}

	return request, nil
}

func (p *Payments) getTransaction(ctx context.Context, transactionId int) (*entity.Transaction, error) {
	if p.database == nil {
		return nil, fmt.Errorf("database not set")
	}
	transaction, err := p.database.GetTransaction(ctx, transactionId)
	if err != nil {
		return nil, fmt.Errorf("failed to get transaction %v", transactionId)
	}
	if !transaction.IsFinished {
		return nil, fmt.Errorf("transaction %v is not finished", transactionId)
	}
	return transaction, nil
}

func (p *Payments) createParameters(parameters *entity.MerchantParameters) (string, error) {
	// convert parameters to JSON string
	parametersJson, err := json.Marshal(parameters)
	if err != nil {
		return "", err
	}
	p.logger.Debug(fmt.Sprintf("request parameters: %s", string(parametersJson)))
	// encode parameters to Base64
	return base64.StdEncoding.EncodeToString(parametersJson), nil
}

// processRequestWithTimeout wraps processRequest with timeout and panic recovery.
// This ensures goroutines don't hang indefinitely and panics are logged.
func (p *Payments) processRequestWithTimeout(parentCtx context.Context, request *entity.PaymentRequest, orderId int) {
	// Recover from panics in goroutine
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("panic in processRequest", fmt.Errorf("panic: %v", r))
		}
	}()

	// Create context with timeout for external API call
	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()

	p.processRequest(ctx, request, orderId)
}

// processResponseWithRecovery wraps processResponse with panic recovery.
func (p *Payments) processResponseWithRecovery(ctx context.Context, response *entity.PaymentParameters) {
	// Recover from panics in goroutine
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("panic in processResponse", fmt.Errorf("panic: %v", r))
		}
	}()

	p.processResponse(ctx, response)
}

// processRequest sends a payment request to Redsys and processes the response.
// This runs in a goroutine to avoid blocking the HTTP handler.
// The context should have a timeout to prevent hanging.
func (p *Payments) processRequest(ctx context.Context, request *entity.PaymentRequest, orderId int) {
	requestData, err := json.Marshal(request)
	if err != nil {
		p.logger.Error("create request", err)
		return
	}

	// Create HTTP request with context for timeout/cancellation support
	req, err := http.NewRequestWithContext(ctx, "POST", p.requestUrl, bytes.NewBuffer(requestData))
	if err != nil {
		p.logger.Error("create http request", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := p.httpClient.Do(req)
	if err != nil {
		// Check if error was due to timeout/cancellation
		if ctx.Err() != nil {
			p.logger.Error("request timeout or cancelled", ctx.Err())
		} else {
			p.logger.Error("post request", err)
		}
		return
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			p.logger.Error("close response body", err)
		}
	}(response.Body)

	body, err := io.ReadAll(response.Body)
	if err != nil {
		p.logger.Error("read response body", err)
		return
	}

	paymentResult, err := p.readResponse(body)
	if err != nil {
		// check if we have an error response from Redsys and close the order
		code, e := p.checkErrorResponse(body)
		if e != nil {
			p.logger.Warn(fmt.Sprintf("unrecognized response: %s", string(body)))
		} else {
			p.logger.Warn(fmt.Sprintf("response error code: %s", code))
			order, _ := p.database.GetPaymentOrder(ctx, orderId)
			if order != nil {
				p.closeOrderOnError(ctx, order, code)
			}
		}
		return
	}

	p.processResponse(ctx, paymentResult)
}

func (p *Payments) readResponse(body []byte) (*entity.PaymentParameters, error) {
	var paymentResponse entity.PaymentRequest
	err := json.Unmarshal(body, &paymentResponse)
	if err != nil {
		return nil, fmt.Errorf("parse response: %v", err)
	}
	return p.readParameters(paymentResponse.Parameters)
}

func (p *Payments) checkErrorResponse(responseBody []byte) (string, error) {
	var errorCode entity.ErrorCodeResponse
	err := json.Unmarshal(responseBody, &errorCode)
	if err != nil {
		return "", err
	}
	return errorCode.Code, nil
}

func (p *Payments) readParameters(parameters string) (*entity.PaymentParameters, error) {
	if parameters == "" {
		return nil, fmt.Errorf("empty parameters")
	}
	parametersBytes, err := base64.StdEncoding.DecodeString(parameters)
	if err != nil {
		return nil, fmt.Errorf("decode parameters: %v", err)
	}
	var paymentResult entity.PaymentParameters
	err = json.Unmarshal(parametersBytes, &paymentResult)
	if err != nil {
		p.logger.Warn(fmt.Sprintf("parameters: %s", string(parametersBytes)))
		return nil, fmt.Errorf("parse parameters: %v", err)
	}
	p.logger.Debug(fmt.Sprintf("received parameters: %s", string(parametersBytes)))
	return &paymentResult, nil
}

func (p *Payments) processResponse(ctx context.Context, paymentResult *entity.PaymentParameters) {
	// Add timeout for database operations if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}

	p.logger.Info(fmt.Sprintf("response: type: %s; result: %s; order: %s; amount: %s", paymentResult.TransactionType, paymentResult.Response, paymentResult.Order, paymentResult.Amount))
	err := p.database.SavePaymentResult(ctx, paymentResult)
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
	order, err := p.database.GetPaymentOrder(ctx, number)
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

		err = p.database.SavePaymentOrder(ctx, order)
		if err != nil {
			p.logger.Error("save payment order", err)
		}
	}

	err = p.checkPaymentResult(paymentResult)
	if err != nil {
		p.closeOrderOnError(ctx, order, paymentResult.Response)
		return
	}
	p.updatePaymentMethodFailCounter(ctx, order.Identifier, 0)

	// if transaction type is 3, then it is a refund
	if paymentResult.TransactionType == "3" {
		order.RefundAmount = amount
		order.RefundTime = time.Now()
		err = p.database.SavePaymentOrder(ctx, order)
		if err != nil {
			p.logger.Error("save payment order", err)
		}
		return
	}

	if order.TransactionId > 0 {

		transaction, e := p.database.GetTransaction(ctx, order.TransactionId)
		if e != nil {
			p.logger.Error("get transaction", e)
			return
		}

		transaction.PaymentOrder = order.Order
		transaction.PaymentBilled = transaction.PaymentBilled + order.Amount
		transaction.PaymentError = ""
		transaction.AddOrder(*order)

		e = p.database.UpdateTransaction(ctx, transaction)
		if e != nil {
			p.logger.Error("update transaction", e)
			return
		}

	} else {

		paymentMethod := entity.PaymentMethod{
			Description: "**** **** **** ****",
			Identifier:  paymentResult.MerchantIdentifier,
			CofTid:      paymentResult.MerchantCofTxnid,
			CardBrand:   paymentResult.CardBrand,
			CardCountry: paymentResult.CardCountry,
			ExpiryDate:  paymentResult.ExpiryDate,
			UserId:      order.UserId,
			UserName:    order.UserName,
		}
		err = p.savePaymentMethod(ctx, &paymentMethod)
		if err != nil {
			p.logger.Error("save payment method", err)
		} else {
			p.logger.Info(fmt.Sprintf("payment method %s saved for %s", secret(paymentMethod.Identifier), order.UserName))
		}

		//after saving payment method, need to refund the amount
		if order.Amount > 0 {
			id := fmt.Sprintf("%d", order.Order)
			err = p.ReturnByOrder(ctx, id, order.Amount)
			if err != nil {
				p.logger.Error("refund payment", err)
				return
			}
		}

	}

}

// closeOrderOnError marks a payment order as failed and closes it.
// This is called when payment processing encounters an error.
func (p *Payments) closeOrderOnError(ctx context.Context, order *entity.PaymentOrder, result string) {
	p.updatePaymentMethodFailCounter(ctx, order.Identifier, 1)

	if !order.IsCompleted {
		order.IsCompleted = true
		order.Result = result
		order.TimeClosed = time.Now()
		if err := p.database.SavePaymentOrder(ctx, order); err != nil {
			p.logger.Error("failed to save payment order on error", err)
		}
	}

	// close transaction on payment error; temporary solution
	if order.TransactionId > 0 {
		p.logger.Info(fmt.Sprintf("close transaction %v on payment error", order.TransactionId))
		transaction, e := p.database.GetTransaction(ctx, order.TransactionId)
		if e != nil {
			p.logger.Error("get transaction", e)
			return
		}
		transaction.PaymentBilled = transaction.PaymentAmount
		transaction.PaymentOrder = order.Order
		transaction.PaymentError = result
		transaction.AddOrder(*order)
		e = p.database.UpdateTransaction(ctx, transaction)
		if e != nil {
			p.logger.Error("update transaction", e)
		}
	}
}

func (p *Payments) savePaymentMethod(ctx context.Context, pm *entity.PaymentMethod) error {
	if pm.UserId == "" {
		return fmt.Errorf("empty user id")
	}
	if pm.Identifier == "" {
		return fmt.Errorf("empty identifier")
	}
	return p.database.SavePaymentMethod(ctx, pm)
}

func (p *Payments) checkPaymentResult(result *entity.PaymentParameters) error {
	if result.TransactionType == "0" {
		if result.Response != "0000" {
			return fmt.Errorf("code %s", result.Response)
		}
		return nil
	}
	if result.TransactionType == "3" {
		if result.Response != "0900" {
			return fmt.Errorf("code %s", result.Response)
		}
		return nil
	}
	return fmt.Errorf("code %s; transaction type %s", result.Response, result.TransactionType)
}

func (p *Payments) updatePaymentMethodFailCounter(ctx context.Context, identifier string, count int) {
	if p.database == nil || identifier == "" {
		return
	}

	paymentMethod, err := p.database.GetPaymentMethodByIdentifier(ctx, identifier)
	if err != nil {
		p.logger.Error("get payment method", err)
		return
	}
	if paymentMethod == nil {
		p.logger.Warn(fmt.Sprintf("payment method %s not found", secret(identifier)))
		return
	}

	if count == 0 {
		paymentMethod.FailCount = 0
	} else {
		paymentMethod.FailCount++
	}
	err = p.database.UpdatePaymentMethodFailCount(ctx, identifier, paymentMethod.FailCount)
	if err != nil {
		p.logger.Error("update payment method", err)
	}
}

func secret(some string) string {
	if len(some) > 5 {
		return fmt.Sprintf("%s***", some[0:5])
	}
	if some == "" {
		return "?"
	}
	return "***"
}
