package checkslip

import "context"

// BanksService lists the banks whose transfers can be looked up.
type BanksService struct{ c *Client }

// SupportedBank is a bank whose transfers can be looked up.
type SupportedBank struct {
	Code    string `json:"code"`
	BOTCode string `json:"bot_code"`
	NameTH  string `json:"name_th"`
	NameEN  string `json:"name_en"`
	// SupportedSince is the day lookups for this bank started.
	SupportedSince Date `json:"supported_since"`
	// LandingPage reports whether checkslip.com has a page for this bank.
	LandingPage bool `json:"landing_page"`
	// LookbackDays is how many days back a transfer can be looked up.
	// Older slips get CodeTooOld.
	LookbackDays int `json:"lookback_days"`
}

// List returns the supported banks. It needs no API key.
func (s *BanksService) List(ctx context.Context, opts ...RequestOption) ([]SupportedBank, error) {
	var out struct {
		Items []SupportedBank `json:"items"`
	}
	req := getReq("/api/banks", nil, opts)
	req.noAuth = true
	if _, err := s.c.doJSON(ctx, req, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}
