package billing

import (
	"errors"
	"fmt"
	"time"
)

// BillingService coordinates payment processing for customer accounts.
type BillingService struct {
	ledger Ledger
	clock  Clock
}

// Ledger records monetary transactions.
type Ledger interface {
	Record(entry LedgerEntry) error
	Balance(accountID string) (int64, error)
}

// Clock abstracts time for testability.
type Clock interface {
	Now() time.Time
}

// LedgerEntry is a single ledger movement.
type LedgerEntry struct {
	AccountID string
	Amount    int64
	Reference string
	At        time.Time
}

// PaymentRequest captures an inbound charge attempt.
type PaymentRequest struct {
	AccountID string
	Amount    int64
	Currency  string
	Method    string
	Metadata  map[string]string
}

// PaymentResult summarizes a completed charge.
type PaymentResult struct {
	TransactionID string
	Status        string
	ProcessedAt   time.Time
}

// NewBillingService wires dependencies for billing operations.
func NewBillingService(ledger Ledger, clock Clock) *BillingService {
	return &BillingService{ledger: ledger, clock: clock}
}

// ProcessPayment validates and records payment processing for a customer charge.
func ProcessPayment(svc *BillingService, req PaymentRequest) (*PaymentResult, error) {
	if req.Amount <= 0 {
		return nil, errors.New("amount must be positive")
	}
	if req.AccountID == "" {
		return nil, errors.New("account id required")
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}

	entry := LedgerEntry{
		AccountID: req.AccountID,
		Amount:    req.Amount,
		Reference: fmt.Sprintf("payment:%s", req.Method),
		At:        svc.clock.Now(),
	}
	if err := svc.ledger.Record(entry); err != nil {
		return nil, fmt.Errorf("record payment: %w", err)
	}

	return &PaymentResult{
		TransactionID: fmt.Sprintf("txn_%s_%d", req.AccountID, entry.At.UnixNano()),
		Status:        "captured",
		ProcessedAt:   entry.At,
	}, nil
}

// RefundPayment reverses a prior capture when permitted.
func RefundPayment(svc *BillingService, accountID, txnID string, amount int64) error {
	if amount <= 0 {
		return errors.New("refund amount must be positive")
	}
	entry := LedgerEntry{
		AccountID: accountID,
		Amount:    -amount,
		Reference: fmt.Sprintf("refund:%s", txnID),
		At:        svc.clock.Now(),
	}
	return svc.ledger.Record(entry)
}

// SummarizeAccount returns a human-readable balance snapshot.
func SummarizeAccount(svc *BillingService, accountID string) (string, error) {
	balance, err := svc.ledger.Balance(accountID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("account=%s balance=%d", accountID, balance), nil
}
