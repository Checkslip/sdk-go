//go:build go1.23

package checkslip_test

import (
	"context"
	"fmt"
	"log"

	"github.com/Checkslip/checkslip-go"
)

func ExampleIter_All() {
	srv := fakeAPI()
	defer srv.Close()
	client := checkslip.NewClient("csk_example", checkslip.WithBaseURL(srv.URL))

	for rec, err := range client.Slips.ListAll(context.Background(), nil).All() {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(rec.RequestID)
	}
	// Output:
	// r1
	// r2
}
