package checkslip_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/Checkslip/sdk-go"
)

// fakeAPI stands in for api.checkslip.com so the examples run offline.
func fakeAPI() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/slips/lookup":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"request_id":"0192d4e0-7c1a-7b3e-9f00-1a2b3c4d5e6f","outcome":"FOUND","checked_at":"2026-10-29T10:41:02+07:00",
"slip":{"trans_ref":"630210534087","transferred_at":"2026-10-29T10:33:30+07:00","amount":{"satang":5000,"currency":"THB"},
"sender":{"bank":{"code":"KKP","bot_code":"069","name_th":null,"name_en":"Kiatnakin Phatra Bank"},"name_th":null,"name_en":"MR. SOMCHAI J","account":"0439"},
"receiver":{"kind":"BANK_ACCOUNT","bank":{"code":"GSB","bot_code":"030","name_th":null,"name_en":"Government Savings Bank"},"name_th":null,"name_en":"MISS SOMYING R","account":"9681"}}}`)
		case "/api/billing/balance":
			w.Header().Set("Content-Type", "application/problem+json")
			w.Header().Set("X-Request-Id", "0192f3a4-7b1c-7d2e-8f30-4a5b6c7d8e9f")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"type":"https://checkslip.com/problems/forbidden","title":"Forbidden","status":403,"code":"FORBIDDEN","instance":"/api/billing/balance"}`)
		case "/api/slips":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("cursor") == "" {
				fmt.Fprint(w, `{"items":[{"request_id":"r1","outcome":"FOUND","checked_at":"2026-11-01T09:00:00Z","channel":"API"}],"next_cursor":"p2"}`)
				return
			}
			fmt.Fprint(w, `{"items":[{"request_id":"r2","outcome":"NOT_FOUND","checked_at":"2026-11-01T08:00:00Z","channel":"API"}],"next_cursor":null}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func ExampleNewClient() {
	client := checkslip.NewClient(os.Getenv("CHECKSLIP_API_KEY"),
		checkslip.WithTimeout(10*time.Second),
		checkslip.WithMaxRetries(3),
		checkslip.WithUserAgent("my-shop/1.4"),
	)
	_ = client
}

func ExampleSlipsService_Lookup() {
	srv := fakeAPI()
	defer srv.Close()
	client := checkslip.NewClient("csk_example", checkslip.WithBaseURL(srv.URL))

	res, err := client.Slips.Lookup(context.Background(), &checkslip.LookupParams{
		QR:              "00330006000001010306902126302105340875102TH9104F86F",
		ClientReference: "order-1001",
	})
	if err != nil {
		fmt.Println(err)
		return
	}
	switch res.Outcome {
	case checkslip.OutcomeFound:
		// The bank returned the transfer. Compare it with your order
		// before you accept the payment.
		fmt.Println(res.Outcome, res.Slip.TransRef, res.Slip.Amount, *res.Slip.Receiver.Account)
	case checkslip.OutcomePending:
		fmt.Println("bank has not shown it yet; read it again after", res.RetryAfter)
	default:
		fmt.Println(res.Outcome)
	}
	// Output: FOUND 630210534087 50.00 THB 9681
}

func ExampleSlipsService_Lookup_image() {
	client := checkslip.NewClient(os.Getenv("CHECKSLIP_API_KEY"))
	f, err := os.Open("slip.jpg")
	if err != nil {
		fmt.Println(err)
		return
	}
	defer f.Close()

	res, err := client.Slips.Lookup(context.Background(), &checkslip.LookupParams{Image: f})
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(res.Outcome)
}

func ExampleWithIdempotencyKey() {
	client := checkslip.NewClient(os.Getenv("CHECKSLIP_API_KEY"))
	// A key tied to your order lets you resend after a crash and get the
	// first answer back, with no second charge.
	res, err := client.Slips.Lookup(context.Background(),
		&checkslip.LookupParams{QR: "..."},
		checkslip.WithIdempotencyKey("order-1001-slip-1"),
	)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(res.Outcome)
}

func ExampleError() {
	srv := fakeAPI()
	defer srv.Close()
	client := checkslip.NewClient("csk_example", checkslip.WithBaseURL(srv.URL))

	_, err := client.Billing.Balance.Get(context.Background())
	var apiErr *checkslip.Error
	if errors.As(err, &apiErr) {
		fmt.Println(apiErr.StatusCode, apiErr.Code, apiErr.RequestID)
	}
	fmt.Println(checkslip.IsForbidden(err))
	// Output:
	// 403 FORBIDDEN 0192f3a4-7b1c-7d2e-8f30-4a5b6c7d8e9f
	// true
}

func ExampleIter() {
	srv := fakeAPI()
	defer srv.Close()
	client := checkslip.NewClient("csk_example", checkslip.WithBaseURL(srv.URL))

	it := client.Slips.ListAll(context.Background(), &checkslip.ListSlipsParams{
		ListOptions: checkslip.ListOptions{Limit: 100},
	})
	for it.Next() {
		rec := it.Current()
		fmt.Println(rec.RequestID, rec.Outcome)
	}
	if err := it.Err(); err != nil {
		fmt.Println(err)
	}
	// Output:
	// r1 FOUND
	// r2 NOT_FOUND
}

func ExampleWebhookVerifier_VerifyRequest() {
	verifier, err := checkslip.NewWebhookVerifier(os.Getenv("CHECKSLIP_WEBHOOK_SECRET"))
	if err != nil {
		fmt.Println(err)
		return
	}
	http.HandleFunc("/hooks/checkslip", func(w http.ResponseWriter, r *http.Request) {
		ev, err := verifier.VerifyRequest(r)
		if err != nil {
			http.Error(w, "bad signature", http.StatusUnauthorized)
			return
		}
		// ev.ID is the same on every resend: skip it if you have seen it.
		if ev.Type == checkslip.EventSlipChecked {
			data, err := ev.SlipChecked()
			if err != nil {
				http.Error(w, "bad payload", http.StatusBadRequest)
				return
			}
			log.Println(data.RequestID, data.Outcome)
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func ExampleWebhookVerifier_Verify() {
	// Values from the Standard Webhooks test vector.
	secret := "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw"
	body := []byte(`{"test": 2432232314}`)
	h := http.Header{}
	h.Set("webhook-id", "msg_p5jXN8AQM9LWM0D4loKWxJek")
	h.Set("webhook-timestamp", "1614265330")
	h.Set("webhook-signature", "v1,g0hM9SsE+OTPJTGt/tmIKtSyZlE3uFJELVlNIOLJ1OE=")

	verifier, err := checkslip.NewWebhookVerifier(secret)
	if err != nil {
		fmt.Println(err)
		return
	}
	// The vector was signed in 2021. With the default 5-minute tolerance
	// it is refused as a replay; turn the check off only to show the
	// signature itself matches.
	fmt.Println(verifier.Verify(h, body))
	fmt.Println(verifier.SetTolerance(0).Verify(h, body))
	// Output:
	// checkslip: webhook timestamp outside tolerance
	// <nil>
}
