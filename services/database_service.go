package services

import "electrum/models"

type Database interface {
	WriteLogMessage(data Data) error

	GetUserTag(idTag string) (*models.UserTag, error)

	GetTransaction(id int) (*models.Transaction, error)

	GetPaymentMethod(userId string) (*models.PaymentMethod, error)

	GetPaymentOrderByTransaction(transactionId int) (*models.PaymentOrder, error)
	SavePaymentOrder(order *models.PaymentOrder) error
	GetLastOrder() (*models.PaymentOrder, error)
}

type Data interface {
	DataType() string
}
