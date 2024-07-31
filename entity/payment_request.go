package entity

type PaymentRequest struct {
	Parameters       string `json:"Ds_MerchantParameters"`
	Signature        string `json:"Ds_Signature"`
	SignatureVersion string `json:"Ds_SignatureVersion"`
}
