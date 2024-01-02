package services

import "electrum/models"

type Database interface {
	WriteLogMessage(data Data) error

	GetUserTag(idTag string) (*models.UserTag, error)

	GetTransaction(id int) (*models.Transaction, error)
	UpdateTransaction(transaction *models.Transaction) error

	GetPaymentMethod(userId string) (*models.PaymentMethod, error)
	SavePaymentMethod(paymentMethod *models.PaymentMethod) error
	GetPaymentMethodByIdentifier(identifier string) (*models.PaymentMethod, error)
	UpdatePaymentMethodFailCount(identifier string, count int) error

	GetPaymentOrderByTransaction(transactionId int) (*models.PaymentOrder, error)
	SavePaymentOrder(order *models.PaymentOrder) error
	GetPaymentOrder(id int) (*models.PaymentOrder, error)
	GetLastOrder() (*models.PaymentOrder, error)
	SavePaymentResult(paymentParameters *models.PaymentParameters) error
}

type Data interface {
	DataType() string
}
