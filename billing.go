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
	// AccountID is the account this balance belongs to.
	AccountID string `json:"account_id"`
	// Credits is all the credits the account has now, the sum of
	// CreditsLeft over every lot in Lots. A slip lookup (looking up the
	// transfer's data with the bank) uses 1 credit only when a matching
	// transfer is found.
	Credits int64 `json:"credits"`
	// Low is true when Credits is below Threshold, meaning credits are
	// running low. It is the same rule as the low-credit email and the web
	// app, so read this value rather than working it out.
	Low bool `json:"low"`
	// Threshold is the credit level below which this account counts as
	// running low (Low, and the low-credit email). It is set from this
	// account's own use. 0 means no warning yet (Low is false).
	Threshold int64 `json:"threshold"`
	// CreditsPerYear is about how many credits this account would use in a
	// year at its recent pace. nil means there has not been enough recent
	// use to tell.
	CreditsPerYear *int64 `json:"credits_per_year"`
	// Lots are the lots that still have credits, in the order they are
	// charged, the next one first. Credits is their sum. Lots with nothing
	// left are not listed.
	Lots []CreditLot `json:"lots,omitempty"`
	// NextExpiresAt is the soonest expiry among lots that still have
	// credits, the date some of the credits start to expire. nil means no
	// credits are left.
	NextExpiresAt *time.Time `json:"next_expires_at"`
	// How Threshold was worked out. Always empty: the API stopped sending
	// this to customers in November 2026, and the published API reference does
	// not list this field. There is no replacement field. It stays here so
	// code that reads it still compiles.
	Basis ThresholdBasis `json:"threshold_basis"`
	// ExpiresAt is the latest expiry among lots that still have credits
	// (after every lot has run out, it still gives the date of the last
	// one). nil means credits were never bought or given. For when the
	// first lot runs out, read NextExpiresAt.
	ExpiresAt *time.Time `json:"expires_at"`
	// UpdatedAt is when this balance last changed.
	UpdatedAt time.Time `json:"updated_at"`
	// LastEntryID is the id of the latest credit entry this balance already
	// counts (the same id as in the credit history). nil means there are no
	// entries yet.
	LastEntryID *string `json:"last_entry_id"`
}

// ThEn is one text in two languages; both are always there.
type ThEn struct {
	// TH is the text in Thai.
	TH string `json:"th"`
	// EN is the text in English.
	EN string `json:"en"`
}

// CreditLot is one lot of credits with its own expiry. The account's credits
// are the sum of its lots. A credit is used only when a matching transfer is
// found, and it is taken from the usable lot that expires first; when two
// expire at the same moment, a campaign lot is used before a bought one.
type CreditLot struct {
	// ID is this lot's id, the same value as CreditEntry.LotID in the
	// credit history.
	ID string `json:"id"`
	// Kind is the kind of lot. New kinds may be added: show an unknown kind
	// as credits with their expiry.
	Kind LotKind `json:"kind"`
	// CampaignID is the campaign that gave this lot, on CAMPAIGN lots only.
	// nil on other lots.
	CampaignID *string `json:"campaign_id"`
	// CampaignName is the campaign's name in Thai and English, as the
	// campaign had it when this account claimed (a later rename does not
	// change it). Both languages always come; pick which one to show for
	// your user. CAMPAIGN lots only; nil on other lots.
	CampaignName *ThEn `json:"campaign_name"`
	// CreditsLeft is the credits this lot has left.
	CreditsLeft int64 `json:"credits_left"`
	// Granted, on a CAMPAIGN lot, is how many credits the campaign gave at
	// the start; with CreditsLeft it shows how many were used. On the PAID
	// lot it is nil, because that lot grows with every purchase.
	Granted *int64 `json:"granted"`
	// ExpiresAt is when the credits left in this lot expire.
	ExpiresAt time.Time `json:"expires_at"`
	// Usable is true when a slip lookup can be charged to this lot now.
	// False when it has expired (and is not yet gone from the list), our
	// team has paused its use, or a campaign lot has used its daily number
	// for today (it works again from 00:00 Thailand time). Meanwhile
	// lookups are charged to another usable lot.
	Usable bool `json:"usable"`
	// UsableToday, on a campaign lot with a daily number, is the credits
	// this lot can still give today, by the Thailand-time day, which starts
	// again at 00:00. nil means this lot has no daily limit.
	UsableToday *int64 `json:"usable_today"`
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
	// FreeReason says why that slip lookup used no credit, on SLIP_FREE
	// entries only. The set is open, so we may add new values: when your
	// code meets a value it does not know, treat it as no credit used
	// rather than letting it break.
	FreeReason FreeReason `json:"free_reason,omitempty"`
	// LotID is the lot this entry added to or took from, the same as a
	// CreditLot.ID in the balance's Lots. nil on entries that did not
	// change credits (Delta 0), on entries from before lots existed, and on
	// entries that changed several lots at once (such as an account
	// deletion or restore).
	LotID *string `json:"lot_id"`
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
	ID string `json:"id"`
	// Code is the order's code for people (ORD-XXXXX-XXXXX), to show and to
	// look the order up instead of ID. Its last character is a check: one
	// wrong character finds nothing rather than a guess.
	Code          string      `json:"code"`
	Status        OrderStatus `json:"status"`
	PackageCode   string      `json:"package_code"`
	PackageNameTH string      `json:"package_name_th"`
	Credits       int64       `json:"credits"`
	Amount        Money       `json:"amount"`
	// VAT is the VAT already inside Amount, fixed when the order was made
	// and not changed by a later rate change. Do not add it to Amount:
	// Amount is what the order asks for. It is nil when the order carries no
	// VAT.
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
	// PriceTotal is what a customer pays for this package now, including any
	// VAT due; do not add VAT again. It equals Price when
	// PackageList.VATRateBP is 0 or PackageList.PricesIncludeVAT is true. An
	// order opened now asks for this amount, so show this one as the price to
	// pay.
	PriceTotal Money `json:"price_total"`
	// PricePerCredit is the price of one credit: Price divided by Credits,
	// rounded half up to a whole satang, so it excludes VAT when
	// PackageList.PricesIncludeVAT is false. Read the amount to pay from
	// PriceTotal; do not multiply this by Credits to get it.
	PricePerCredit Money `json:"price_per_credit"`
	// PricePerCreditBaht is the same price per credit as PricePerCredit, in
	// baht, as display text with three decimals, rounded half up at a tenth
	// of a satang.
	//
	// Two packages that tie at a whole satang differ here (0.175 against
	// 0.167). Show it as sent instead of computing it. It divides Price, not
	// PriceTotal, so it excludes VAT when PackageList.PricesIncludeVAT is
	// false.
	PricePerCreditBaht string `json:"price_per_credit_baht"`
	Credits            int64  `json:"credits"`
	// ValidityDays is how long the credits last. Credits from every
	// package join one balance that lasts from the latest purchase.
	ValidityDays int  `json:"validity_days"`
	ForSale      bool `json:"for_sale"`
	Sort         int  `json:"sort"`
	// Offer is a time-limited lower price running now, or nil when there is
	// none. Price stays the regular price; an order opened before
	// Offer.EndsAt asks for the offer price, which PriceTotal shows. After
	// Offer.EndsAt the regular price applies again on its own.
	Offer *PackageOffer `json:"offer"`
}

// PackageOffer is the time-limited price of a Package while an offer runs.
type PackageOffer struct {
	// Price is the offer price of the package.
	Price Money `json:"price"`
	// PricePerCredit is the offer price of one credit.
	PricePerCredit Money `json:"price_per_credit"`
	// PricePerCreditBaht is the offer price per credit in baht, as display
	// text with three decimals.
	PricePerCreditBaht string `json:"price_per_credit_baht"`
	// EndsAt is when the offer stops and the regular price applies again.
	EndsAt time.Time `json:"ends_at"`
}

// PackageList is the list of credit packages.
type PackageList struct {
	// CheckoutOpen reports whether packages can be bought right now.
	CheckoutOpen     bool `json:"checkout_open"`
	PricesIncludeVAT bool `json:"prices_include_vat"`
	// VATRateBP is the VAT rate in force now, in basis points: 700 is 7%,
	// and 0 means no VAT applies to any package or order yet. Use it to show
	// the rate and to tell whether VAT is in effect; read the amount to pay
	// from each package's PriceTotal instead of computing VAT yourself.
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
	SellerReady bool   `json:"seller_ready"`
	ReceiptNo   string `json:"receipt_no"`
	OrderID     string `json:"order_id"`
	// OrderCode is the order code (ORD-XXXXX-XXXXX) of this receipt's
	// order, the same as Order.Code, for referring to the order. The
	// receipt's legal number is ReceiptNo; never use this code in its
	// place. Every receipt comes from an order and every order has a code,
	// so it should always be set; nil means the order's code could not be
	// read at that moment.
	OrderCode     *string       `json:"order_code"`
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
