package services

import "electrum/entity"

type Database interface {
	WriteLogMessage(data Data) error

	GetUserTag(idTag string) (*entity.UserTag, error)

	GetTransaction(id int) (*entity.Transaction, error)
	UpdateTransaction(transaction *entity.Transaction) error

	GetPaymentMethod(userId string) (*entity.PaymentMethod, error)
	SavePaymentMethod(paymentMethod *entity.PaymentMethod) error
	GetPaymentMethodByIdentifier(identifier string) (*entity.PaymentMethod, error)
	UpdatePaymentMethodFailCount(identifier string, count int) error

	GetPaymentOrderByTransaction(transactionId int) (*entity.PaymentOrder, error)
	SavePaymentOrder(order *entity.PaymentOrder) error
	GetPaymentOrder(id int) (*entity.PaymentOrder, error)
	GetLastOrder() (*entity.PaymentOrder, error)
	SavePaymentResult(paymentParameters *entity.PaymentParameters) error
}

type Data interface {
	DataType() string
}
