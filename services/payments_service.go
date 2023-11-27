package services

type Payments interface {
	PayTransaction(transactionId int) error
	ReturnPayment(transactionId int) error
}
