package entity

// MerchantParameters represents Redsys API request parameters for payment operations.
// These parameters are Base64-encoded and signed with HMAC-SHA256 before sending to Redsys.
type MerchantParameters struct {
	// Amount in cents (e.g., "1000" = 10.00 EUR)
	Amount string `json:"DS_MERCHANT_AMOUNT"`
	// Order number - must be unique across the system (4-12 digits)
	Order string `json:"DS_MERCHANT_ORDER"`
	// Identifier for stored payment method (card token)
	Identifier string `json:"DS_MERCHANT_IDENTIFIER"`
	// Merchant code assigned by Redsys
	MerchantCode string `json:"DS_MERCHANT_MERCHANTCODE"`
	// Currency code (978 = EUR)
	Currency string `json:"DS_MERCHANT_CURRENCY"`
	// Transaction type: "0" = Authorization, "3" = Refund
	TransactionType string `json:"DS_MERCHANT_TRANSACTIONTYPE"`
	// Terminal number assigned by Redsys
	Terminal string `json:"DS_MERCHANT_TERMINAL"`
	// DirectPayment: "true" = use stored token without redirect, "false" = redirect customer
	DirectPayment string `json:"DS_MERCHANT_DIRECTPAYMENT"`
	// Exception: "MIT" = Merchant Initiated Transaction exemption (PSD2)
	Exception string `json:"DS_MERCHANT_EXCEP_SCA"`
	// CofIni: "S" = initial credential storage, "N" = subsequent use of stored credentials
	CofIni string `json:"DS_MERCHANT_COF_INI"`
	// CofType: "R" = Recurring, "I" = Installments, "C" = Others
	CofType string `json:"DS_MERCHANT_COF_TYPE"`
	// CofTid: Network transaction ID from initial authorization (links MIT to original CIT)
	CofTid string `json:"DS_MERCHANT_COF_TXNID"`
}
