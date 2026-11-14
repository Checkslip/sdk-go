package checkslip

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
)

// BillingService reads credits, orders, receipts and the billing details
// printed on receipts.
type BillingService struct {
	c *Client
	// Balance is the credit left on the account.
	Balance *BillingBalanceService
	// Info is the billing details printed on receipts.
	Info *BillingInfoService
	// List are the credit packages on sale.
	Packages *BillingPackagesService
	// List is every top-up, charge and expiry.
	Ledger *BillingLedgerService
	// List are orders for credit packages.
	Orders *BillingOrdersService
	// List are receipts for paid orders.
	Receipts *BillingReceiptsService
}

// BillingBalanceService reads the credit balance.
type BillingBalanceService struct{ c *Client }

// BillingInfoService reads and replaces the billing details on receipts.
type BillingInfoService struct{ c *Client }

// BillingPackagesService lists the credit packages on sale.
type BillingPackagesService struct{ c *Client }

// BillingLedgerService reads the credit ledger.
type BillingLedgerService struct{ c *Client }

// BillingOrdersService reads orders and refreshes their payment result.
type BillingOrdersService struct{ c *Client }

// BillingReceiptsService reads receipts.
type BillingReceiptsService struct{ c *Client }

// Balance is the account's credit balance.
type Balance struct {
	AccountID string `json:"account_id"`
	// Credits is the number of lookups left. One FOUND lookup uses one.
	Credits int64 `json:"credits"`
	// Low is true when Credits has fallen to Threshold or below. Show it as
	// it is rather than computing your own threshold.
	Low       bool           `json:"low"`
	Threshold int64          `json:"threshold"`
	Basis     ThresholdBasis `json:"threshold_basis"`
	// ExpiresAt is when the remaining credits expire, or nil.
	ExpiresAt   *time.Time `json:"expires_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastEntryID *string    `json:"last_entry_id"`
}

// Get returns the remaining credits, their expiry and whether they are
// running low. It needs an API key with credit:read or billing:read.
func (s *BillingBalanceService) Get(ctx context.Context, opts ...RequestOption) (*Balance, error) {
	var out Balance
	if _, err := s.c.doJSON(ctx, getReq("/api/billing/balance", nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BillingInfo is the buyer shown on receipts.
type BillingInfo struct {
	BuyerType BuyerType `json:"buyer_type,omitempty"`
	Name      string    `json:"name,omitempty"`
	TaxID     string    `json:"tax_id,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Address   string    `json:"address,omitempty"`
	Email     string    `json:"email,omitempty"`
}

// Get returns the billing details used on receipts.
func (s *BillingInfoService) Get(ctx context.Context, opts ...RequestOption) (*BillingInfo, error) {
	var out BillingInfo
	if _, err := s.c.doJSON(ctx, getReq("/api/billing/billing-info", nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Update saves the buyer details printed on your next receipts. Receipts
// already issued keep theirs. The tax ID must be 13 digits. It needs an API
// key with billing:write.
func (s *BillingInfoService) Update(ctx context.Context, info *BillingInfo, opts ...RequestOption) (*BillingInfo, error) {
	if info == nil {
		return nil, errors.New("checkslip: billing info is required")
	}
	body, err := jsonBody(info)
	if err != nil {
		return nil, err
	}
	var out BillingInfo
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPut, path: "/api/billing/billing-info",
		body: body, ctype: "application/json", idempotent: true, opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreditEntry is one change of the credit balance.
type CreditEntry struct {
	ID string `json:"id"`
	// Delta is the change: positive for credits added, negative for used
	// or expired.
	Delta          int64        `json:"delta"`
	BalanceAfter   int64        `json:"balance_after"`
	ExpiresAtAfter *time.Time   `json:"expires_at_after,omitempty"`
	Reason         CreditReason `json:"reason"`
	FreeReason     FreeReason   `json:"free_reason,omitempty"`
	// RefType and RefID point at what caused the change, such as an order
	// or a lookup.
	RefType   string    `json:"ref_type,omitempty"`
	RefID     string    `json:"ref_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// LedgerParams filters the credit ledger.
type LedgerParams struct {
	ListOptions
	// Reason keeps only these reasons.
	Reason []CreditReason
	// From and To limit the time range. Zero means no limit.
	From, To time.Time
}

// List returns one page of credit balance changes, newest first:
// purchases, lookups that used a credit, lookups that did not (with the
// reason in FreeReason) and credits that expired. One call covers at most 31
// days.
func (s *BillingLedgerService) List(ctx context.Context, params *LedgerParams, opts ...RequestOption) (*Page[CreditEntry], error) {
	q := query{}
	if params != nil {
		q.page(params.ListOptions)
		if len(params.Reason) > 0 {
			parts := make([]string, len(params.Reason))
			for i, r := range params.Reason {
				parts[i] = string(r)
			}
			q.str("reason", strings.Join(parts, ","))
		}
		q.time("from", params.From)
		q.time("to", params.To)
	}
	var out Page[CreditEntry]
	if _, err := s.c.doJSON(ctx, getReq("/api/billing/ledger", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAll walks every ledger entry that matches params.
func (s *BillingLedgerService) ListAll(ctx context.Context, params *LedgerParams, opts ...RequestOption) *Iter[CreditEntry] {
	p := LedgerParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[CreditEntry], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// Order is a purchase of credits.
type Order struct {
	ID            string      `json:"id"`
	Status        OrderStatus `json:"status"`
	PackageCode   string      `json:"package_code"`
	PackageNameTH string      `json:"package_name_th"`
	Credits       int64       `json:"credits"`
	Amount        Money       `json:"amount"`
	// VAT is the VAT already inside Amount, fixed when the order was made.
	// Do not add it to Amount: Amount is what the order asks for. It is nil
	// when the order carries no VAT.
	VAT           *Money        `json:"vat,omitempty"`
	PaymentMethod PaymentMethod `json:"payment_method,omitempty"`
	ReceiptID     *string       `json:"receipt_id,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	PaidAt        *time.Time    `json:"paid_at,omitempty"`
	CancelledAt   *time.Time    `json:"cancelled_at,omitempty"`
	// Confirming is true when the payment page was closed but the money
	// has not arrived yet.
	Confirming   bool    `json:"confirming,omitempty"`
	ReviewReason *string `json:"review_reason,omitempty"`
}

// OrdersParams filters the order list.
type OrdersParams struct {
	ListOptions
	Status OrderStatus
}

// List returns one page of credit orders, newest first.
func (s *BillingOrdersService) List(ctx context.Context, params *OrdersParams, opts ...RequestOption) (*Page[Order], error) {
	q := query{}
	if params != nil {
		q.page(params.ListOptions)
		q.str("status", string(params.Status))
	}
	var out Page[Order]
	if _, err := s.c.doJSON(ctx, getReq("/api/billing/orders", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAll walks every order that matches params.
func (s *BillingOrdersService) ListAll(ctx context.Context, params *OrdersParams, opts ...RequestOption) *Iter[Order] {
	p := OrdersParams{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[Order], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// Get returns one credit order.
func (s *BillingOrdersService) Get(ctx context.Context, orderID string, opts ...RequestOption) (*Order, error) {
	if orderID == "" {
		return nil, errors.New("checkslip: order ID is required")
	}
	var out Order
	if _, err := s.c.doJSON(ctx, getReq("/api/billing/orders/"+pathEscape(orderID), nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Sync asks the payment provider for the payment result of an order now
// instead of waiting for its notice, and returns the order. If it is paid,
// the credits arrive then. If the payment page was closed but the money has
// not arrived, the order shows Confirming. Asking again within a few seconds
// returns the same result.
func (s *BillingOrdersService) Sync(ctx context.Context, orderID string, opts ...RequestOption) (*Order, error) {
	if orderID == "" {
		return nil, errors.New("checkslip: order ID is required")
	}
	var out Order
	if _, err := s.c.doJSON(ctx, &request{
		method: http.MethodPost, path: "/api/billing/orders/" + pathEscape(orderID) + "/sync",
		// The API documents a repeated sync as returning the same result.
		idempotent: true, opts: opts,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Package is a credit package for sale.
type Package struct {
	Code   string `json:"code"`
	NameTH string `json:"name_th"`
	NameEN string `json:"name_en"`
	Price  Money  `json:"price"`
	// PriceTotal is what a customer pays for this package now: Price plus
	// VAT when VAT is in effect and prices exclude it; otherwise it is the
	// same as Price. An order opened now asks for this amount, so show this
	// one as the price to pay.
	PriceTotal     Money `json:"price_total"`
	PricePerCredit Money `json:"price_per_credit"`
	// PricePerCreditBaht is the same price per credit, in baht, as text
	// with three decimals, rounded half up at a tenth of a satang. Two
	// packages that tie at a whole satang differ here (0.175 against
	// 0.167). Show it as sent rather than working it out from Price and
	// Credits. Like PricePerCredit it divides Price, not PriceTotal, so it
	// excludes VAT whenever Price does.
	PricePerCreditBaht string `json:"price_per_credit_baht"`
	Credits            int64  `json:"credits"`
	// ValidityDays is how long the credits last. Credits from every
	// package join one balance that lasts from the latest purchase.
	ValidityDays int  `json:"validity_days"`
	ForSale      bool `json:"for_sale"`
	Sort         int  `json:"sort"`
}

// PackageList is the list of credit packages.
type PackageList struct {
	// CheckoutOpen reports whether packages can be bought right now.
	CheckoutOpen     bool `json:"checkout_open"`
	PricesIncludeVAT bool `json:"prices_include_vat"`
	// VATRateBP is the VAT rate in force now, in basis points: 700 is 7%,
	// and 0 means no VAT applies anywhere yet. Read it to find out whether
	// VAT is in effect.
	VATRateBP int       `json:"vat_rate_bp"`
	Items     []Package `json:"items"`
}

// List returns every credit package with its price, credits and
// validity. One credit covers one transfer lookup.
func (s *BillingPackagesService) List(ctx context.Context, opts ...RequestOption) (*PackageList, error) {
	var out PackageList
	if _, err := s.c.doJSON(ctx, getReq("/api/billing/packages", nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Receipt is a tax receipt for a paid order.
type Receipt struct {
	ID string `json:"id"`
	// SellerReady reports whether the seller details are complete.
	SellerReady   bool          `json:"seller_ready"`
	ReceiptNo     string        `json:"receipt_no"`
	OrderID       string        `json:"order_id"`
	IssuedAt      time.Time     `json:"issued_at"`
	Seller        ReceiptSeller `json:"seller"`
	Buyer         BillingInfo   `json:"buyer"`
	Lines         []ReceiptLine `json:"lines"`
	VAT           *ReceiptVAT   `json:"vat,omitempty"`
	Total         Money         `json:"total"`
	PaymentMethod PaymentMethod `json:"payment_method"`
}

// ReceiptSeller is the seller printed on a receipt.
type ReceiptSeller struct {
	Name    string `json:"name"`
	TaxID   string `json:"tax_id,omitempty"`
	Address string `json:"address,omitempty"`
	Branch  string `json:"branch,omitempty"`
}

// ReceiptLine is one line of a receipt.
type ReceiptLine struct {
	Description string `json:"description"`
	Quantity    int    `json:"quantity"`
	Amount      Money  `json:"amount"`
}

// ReceiptVAT is the VAT on a receipt.
type ReceiptVAT struct {
	// RateBP is the rate in basis points: 700 is 7%.
	RateBP int   `json:"rate_bp"`
	Base   Money `json:"base"`
	Amount Money `json:"amount"`
}

// List returns one page of receipts, newest first.
func (s *BillingReceiptsService) List(ctx context.Context, params *ListOptions, opts ...RequestOption) (*Page[Receipt], error) {
	q := query{}
	if params != nil {
		q.page(*params)
	}
	var out Page[Receipt]
	if _, err := s.c.doJSON(ctx, getReq("/api/billing/receipts", q.values(), opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListAll walks every receipt.
func (s *BillingReceiptsService) ListAll(ctx context.Context, params *ListOptions, opts ...RequestOption) *Iter[Receipt] {
	p := ListOptions{}
	if params != nil {
		p = *params
	}
	return newIter(ctx, p.Cursor, func(ctx context.Context, cursor string) (*Page[Receipt], error) {
		p.Cursor = cursor
		return s.List(ctx, &p, opts...)
	})
}

// Get returns one receipt.
func (s *BillingReceiptsService) Get(ctx context.Context, receiptID string, opts ...RequestOption) (*Receipt, error) {
	if receiptID == "" {
		return nil, errors.New("checkslip: receipt ID is required")
	}
	var out Receipt
	if _, err := s.c.doJSON(ctx, getReq("/api/billing/receipts/"+pathEscape(receiptID), nil, opts), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
