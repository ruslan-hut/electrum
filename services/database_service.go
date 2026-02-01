package services

import (
	"context"
	"electrum/entity"
)

// Database provides database operations for the payment service.
// All methods accept context.Context as the first parameter for proper
// timeout, cancellation, and request tracing support.
type Database interface {
	WriteLogMessage(ctx context.Context, data Data) error

	GetUserTag(ctx context.Context, idTag string) (*entity.UserTag, error)

	GetTransaction(ctx context.Context, id int) (*entity.Transaction, error)
	UpdateTransaction(ctx context.Context, transaction *entity.Transaction) error

	GetPaymentMethod(ctx context.Context, userId string) (*entity.PaymentMethod, error)
	SavePaymentMethod(ctx context.Context, paymentMethod *entity.PaymentMethod) error
	GetPaymentMethodByIdentifier(ctx context.Context, identifier string) (*entity.PaymentMethod, error)
	UpdatePaymentMethodFailCount(ctx context.Context, identifier string, count int) error

	GetPaymentOrderByTransaction(ctx context.Context, transactionId int) (*entity.PaymentOrder, error)
	SavePaymentOrder(ctx context.Context, order *entity.PaymentOrder) error
	GetPaymentOrder(ctx context.Context, id int) (*entity.PaymentOrder, error)
	GetLastOrder(ctx context.Context) (*entity.PaymentOrder, error)
	SavePaymentResult(ctx context.Context, paymentParameters *entity.PaymentParameters) error
}

type Data interface {
	DataType() string
}
