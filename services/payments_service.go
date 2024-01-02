package services

type Payments interface {
	Notify(data []byte) error
	PayTransaction(transactionId int) error
	ReturnPayment(transactionId int) error
	ReturnByOrder(orderId string, amount int) error
}
