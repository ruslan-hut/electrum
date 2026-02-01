package services

import "context"

// Payments provides payment processing operations.
// All methods accept context.Context for proper timeout and cancellation support.
type Payments interface {
	Notify(ctx context.Context, data []byte) error
	PayTransaction(ctx context.Context, transactionId int) error
	ReturnPayment(ctx context.Context, transactionId int) error
	ReturnByOrder(ctx context.Context, orderId string, amount int) error
}
