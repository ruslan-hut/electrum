// Package entity defines data models for the Electrum payment service.
package entity

import (
	"sync"
	"time"
)

// Transaction represents an electric vehicle charging session with payment tracking.
// It tracks energy consumption, payment status, and associated payment orders.
type Transaction struct {
	Id            int                `json:"transaction_id" bson:"transaction_id"`
	SessionId     string             `json:"session_id" bson:"session_id"`
	IsFinished    bool               `json:"is_finished" bson:"is_finished"`
	ConnectorId   int                `json:"connector_id" bson:"connector_id"`
	ChargePointId string             `json:"charge_point_id" bson:"charge_point_id"`
	IdTag         string             `json:"id_tag" bson:"id_tag"`
	ReservationId *int               `json:"reservation_id,omitempty" bson:"reservation_id"`
	MeterStart    int                `json:"meter_start" bson:"meter_start"`
	MeterStop     int                `json:"meter_stop" bson:"meter_stop"`
	TimeStart     time.Time          `json:"time_start" bson:"time_start"`
	TimeStop      time.Time          `json:"time_stop" bson:"time_stop"`
	Reason        string             `json:"reason" bson:"reason"`
	IdTagNote     string             `json:"id_tag_note" bson:"id_tag_note"`
	Username      string             `json:"username" bson:"username"`
	PaymentAmount int                `json:"payment_amount" bson:"payment_amount"`
	PaymentBilled int                `json:"payment_billed" bson:"payment_billed"`
	PaymentOrder  int                `json:"payment_order" bson:"payment_order"`
	PaymentError  string             `json:"payment_error" bson:"payment_error"`
	Plan          PaymentPlan        `json:"payment_plan" bson:"payment_plan"`
	MeterValues   []TransactionMeter `json:"meter_values" bson:"meter_values"`
	PaymentMethod *PaymentMethod     `json:"payment_method,omitempty" bson:"payment_method"`
	PaymentOrders []PaymentOrder     `json:"payment_orders" bson:"payment_orders"`
	UserTag       *UserTag           `json:"user_tag,omitempty" bson:"user_tag"`

	// mutex provides thread-safe access to transaction data.
	// Changed from *sync.Mutex to sync.Mutex to ensure it's always initialized.
	// Note: This field is not serialized to JSON/BSON (no tags).
	mutex sync.Mutex
}

// Lock acquires the transaction mutex for thread-safe operations.
// Use with defer Unlock() to ensure the lock is released.
func (t *Transaction) Lock() {
	t.mutex.Lock()
}

// Unlock releases the transaction mutex.
func (t *Transaction) Unlock() {
	t.mutex.Unlock()
}

// AddOrder adds a payment order to the transaction if it doesn't already exist.
// Orders are identified by their Order number to prevent duplicates.
func (t *Transaction) AddOrder(order PaymentOrder) {
	for _, paymentOrder := range t.PaymentOrders {
		if paymentOrder.Order == order.Order {
			return
		}
	}
	t.PaymentOrders = append(t.PaymentOrders, order)
}
