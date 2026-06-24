package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/example/fixture/src/billing"
)

// PaymentHandler exposes HTTP endpoints for billing workflows.
type PaymentHandler struct {
	service *billing.BillingService
}

// NewPaymentHandler wires billing into HTTP handlers.
func NewPaymentHandler(service *billing.BillingService) *PaymentHandler {
	return &PaymentHandler{service: service}
}

// HandleCharge accepts JSON payment requests and returns a receipt payload.
func (h *PaymentHandler) HandleCharge(w http.ResponseWriter, r *http.Request) {
	var req billing.PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	result, err := billing.ProcessPayment(h.service, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

// HandleRefund accepts partial or full refund instructions.
func (h *PaymentHandler) HandleRefund(w http.ResponseWriter, r *http.Request) {
	type body struct {
		AccountID     string `json:"account_id"`
		TransactionID string `json:"transaction_id"`
		Amount        int64  `json:"amount"`
	}
	var payload body
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if err := billing.RefundPayment(h.service, payload.AccountID, payload.TransactionID, payload.Amount); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
