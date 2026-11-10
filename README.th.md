# Checkslip Go SDK

[English](README.md) · [เอกสาร API](https://checkslip.com/docs) · [API reference](https://checkslip.com/docs/reference) · [Go reference](https://pkg.go.dev/github.com/Checkslip/sdk-go)

ไลบรารี Go อย่างเป็นทางการสำหรับ API ของ [Checkslip](https://checkslip.com)

Checkslip ค้นหารายการโอนเงินของธนาคารไทยจากสลิป คุณส่งข้อความ QR บนสลิปหรือรูปสลิปเข้ามา Checkslip จะถามธนาคารผู้โอนถึงรายการโอนนั้น แล้วส่งข้อมูลที่ธนาคารมีกลับไปให้ การจะรับชำระหรือไม่เป็นการตัดสินใจของคุณ ให้เทียบยอดเงิน บัญชีผู้รับ และเวลาโอนกับคำสั่งซื้อของคุณเอง

ใช้ได้กับ Go 1.22 ขึ้นไป และไม่มี dependency นอกจาก standard library

## ติดตั้ง

```sh
go get github.com/Checkslip/sdk-go
```

## เริ่มใช้งาน

ออก API key ได้ในเว็บแอปของ Checkslip key ขึ้นต้นด้วย `csk_` อย่าเขียน key ไว้ในซอร์สโค้ด ให้ส่งเข้ามาทาง environment หรือ secret manager

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/Checkslip/sdk-go"
)

func main() {
	client := checkslip.NewClient(os.Getenv("CHECKSLIP_API_KEY"))

	res, err := client.Slips.Lookup(context.Background(), &checkslip.LookupParams{
		QR:              "00330006000001010306902126302105340875102TH9104F86F",
		ClientReference: "order-1001", // ไม่บังคับ ค่านี้จะกลับมาในประวัติและ webhook
	})
	if err != nil {
		log.Fatal(err)
	}

	switch res.Outcome {
	case checkslip.OutcomeFound:
		fmt.Println("ยอดเงิน:", res.Slip.Amount)
		fmt.Println("เวลาโอน:", res.Slip.TransferredAt)
		fmt.Println("บัญชีผู้รับ:", *res.Slip.Receiver.Account)
	case checkslip.OutcomeNotFound:
		fmt.Println("ธนาคารไม่มีรายการโอนของเลขอ้างอิงนี้")
	case checkslip.OutcomeDuplicate:
		fmt.Println("เคยค้นหาแล้ว ครั้งแรกเมื่อ", res.DuplicateOf.CheckedAt)
	case checkslip.OutcomePending:
		fmt.Println("ธนาคารยังไม่แสดงรายการนี้ ถามใหม่หลัง", res.RetryAfter)
	}
}
```

หักเครดิตเฉพาะเมื่อผลเป็น `FOUND`

ถ้าจะส่งรูปแทนข้อความ QR ให้ตั้งค่า `Image` ใน `LookupParams` แทน `QR` รับไฟล์ JPEG, PNG และ WebP ขนาดไม่เกิน 10 MB และ Checkslip ไม่เก็บรูปไว้ ถ้าใส่มาทั้งคู่หรือไม่ใส่เลย ไคลเอนต์จะแจ้ง error ก่อนส่งคำขอ

```go
f, err := os.Open("slip.jpg")
if err != nil {
	return err
}
defer f.Close()

res, err := client.Slips.Lookup(ctx, &checkslip.LookupParams{Image: f})
```

ผล `PENDING` จะได้คำตอบทีหลัง อ่านผลด้วย `client.Slips.Get(ctx, res.RequestID)` หลังเวลาใน `res.RetryAfter` หรือรอ webhook `slip.checked`

## ฟังก์ชันที่มี

| Service | Method |
| --- | --- |
| `client.Slips` | `Lookup`, `Get`, `List`, `ListAll`, `Counts` และ `Exports` (`Create`, `Get`, `DownloadPart`) |
| `client.Banks` | `List` |
| `client.Billing` | `Balance.Get`, `Info` (`Get`, `Update`), `Packages.List`, `Ledger` (`List`, `ListAll`), `Orders` (`List`, `ListAll`, `Get`, `Sync`), `Receipts` (`List`, `ListAll`, `Get`) |
| `client.Usage` | `Get` |
| `client.Audit` | `Events` (`List`, `ListAll`, `Get`), `Requests` (`List`, `ListAll`, `Get`), `Checkpoints` (`List`, `ListAll`, `ListLatest`), `Chain.Check`, `Exports` (`List`, `ListAll`, `Create`, `Get`, `Download`), `SigningKeys.Get` |
| `client.Webhooks` | `Verify`, `EventTypes.List`, `Endpoints` (`List`, `ListAll`, `Create`, `Get`, `Update`, `Delete`, `Recover`, `RotateSecret`, `SendTest`), `Deliveries` (`List`, `ListAll`, `Get`, `Redeliver`) |

ชื่อชุดเดียวกันและโครงเดียวกันนี้อยู่ในไคลเอนต์ JavaScript, PHP และ Rust ด้วย

ทุก method รับ `context.Context` เป็นพารามิเตอร์แรก ใช้กำหนดเวลาหมดและยกเลิกคำขอ

## ข้อผิดพลาด

คำตอบที่มีสถานะ 400 ขึ้นไปจะกลับมาเป็น `*checkslip.Error` ซึ่งเก็บรายละเอียดตาม [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) ที่ API ส่งมา

```go
res, err := client.Slips.Lookup(ctx, params)
if err != nil {
	var apiErr *checkslip.Error
	if errors.As(err, &apiErr) {
		log.Printf("status=%d code=%s title=%q request=%s",
			apiErr.StatusCode, apiErr.Code, apiErr.Title, apiErr.RequestID)
	}

	switch {
	case checkslip.HasCode(err, checkslip.CodeInvalidQR):
		// อ่าน QR นี้เป็น QR สลิปไม่ได้
	case checkslip.HasCode(err, checkslip.CodeTooOld):
		// เกินช่วงที่ธนาคารให้ค้นย้อนหลัง
	case checkslip.IsNoCredit(err):
		// เติมเครดิต
	case checkslip.IsRateLimited(err):
		// รอตาม apiErr.RetryAfter
	}
	return err
}
```

ถ้าติดต่อฝ่ายช่วยเหลือของ Checkslip ให้แจ้ง `RequestID` ไปด้วย QR ที่ใช้ไม่ได้ (`INVALID_QR`, `QR_TAMPERED`, `TOO_OLD`, `INVALID_FILE`) จะได้ error กลับไปและไม่หักเครดิต

ฟังก์ชันช่วยที่มีคือ `IsRateLimited`, `IsUnauthenticated`, `IsForbidden`, `IsNotFound`, `IsNoCredit`, `IsValidation`, `IsServerError`, `HasCode` และ `AsError` ถ้าเชื่อมต่อไม่สำเร็จหรือ context ถูกยกเลิก จะได้ error ธรรมดา ไม่ใช่ `*checkslip.Error`

## การส่งซ้ำอัตโนมัติ

เมื่อคำขอล้มเหลว client จะส่งใหม่ได้อีกสูงสุด 2 ครั้ง (ค่าเริ่มต้น) โดยรอนานขึ้นทุกรอบ เริ่มที่ 0.5 วินาทีและเพิ่มเท่าตัวจนถึง 8 วินาที พร้อมสุ่มระยะรอเล็กน้อย

- `429 Too Many Requests` ส่งซ้ำเสมอ หลังเวลาใน `Retry-After` ที่ API ส่งมา
- `500`, `502`, `503`, `504` และการเชื่อมต่อล้มเหลว ส่งซ้ำเฉพาะคำขอที่ส่งสองครั้งแล้วงานไม่เกิดซ้ำ คือ `GET`, `PUT`, `DELETE` และคำขอที่มี `Idempotency-Key`
- `409 IDEMPOTENCY_IN_FLIGHT` ส่งซ้ำด้วย key เดิมระหว่างที่คำขอแรกยังทำอยู่

ถ้า `Retry-After` นานกว่า 60 วินาที หรือนานกว่าเวลาที่เหลือใน context ตัว client จะไม่รอ แต่คืน error ทันทีพร้อมค่า `RetryAfter`

## Idempotency

`Slips.Lookup` ส่ง `Idempotency-Key` ทุกครั้ง คำขอที่ API บังคับให้มี key ก็เช่นกัน ได้แก่ สร้างไฟล์หลักฐาน, recover, เปลี่ยน secret, ส่งข้อความทดสอบ และส่ง webhook ซ้ำ ถ้าคุณไม่ได้ส่ง key มา client จะสุ่มให้และใช้ key เดิมเมื่อส่งซ้ำเอง การค้นหาที่ส่งซ้ำด้วย key เดิมจะได้คำตอบแรกกลับมาและไม่หักเครดิตซ้ำ

ถ้าโปรแกรมของคุณอาจส่งคำขอเดิมอีกหลังรีสตาร์ต ให้กำหนด key เอง เช่นสร้างจากเลขคำสั่งซื้อ

```go
res, err := client.Slips.Lookup(ctx, params,
	checkslip.WithIdempotencyKey("order-1001-slip-1"))
```

key ที่ส่งไปอยู่ใน `res.IdempotencyKey` และใน `apiErr.IdempotencyKey` เมื่อคำขอล้มเหลว key ยาว 1 ถึง 255 ตัวอักษร ASCII ที่พิมพ์ได้

## การแบ่งหน้า

method `List` คืนข้อมูลทีละหน้าเป็น `Page` ส่ง `NextCursor` กลับไปใน `ListOptions.Cursor` เพื่อขอหน้าถัดไป หรือให้ `ListAll` ทำให้

```go
it := client.Slips.ListAll(ctx, &checkslip.ListSlipsParams{
	Outcome: []checkslip.Outcome{checkslip.OutcomeFound},
})
for it.Next() {
	rec := it.Current()
	fmt.Println(rec.RequestID, rec.CheckedAt)
}
if err := it.Err(); err != nil {
	return err
}
```

ใน Go 1.23 ขึ้นไปใช้ range ได้

```go
for rec, err := range client.Slips.ListAll(ctx, nil).All() {
	if err != nil {
		return err
	}
	fmt.Println(rec.RequestID)
}
```

`it.Cursor()` คืน cursor ของหน้าถัดไป ใช้หยุดแล้วอ่านต่อภายหลังได้

## Webhook

Checkslip เซ็นข้อความ webhook ตาม [Standard Webhooks](https://www.standardwebhooks.com) v1 ทุกคำขอมี header `webhook-id`, `webhook-timestamp` และ `webhook-signature` ลายเซ็นคือ HMAC-SHA256 ของ `{webhook-id}.{webhook-timestamp}.{raw body}` โดยใช้ secret ของ endpoint (`whsec_...`) เป็นกุญแจ

ตรวจลายเซ็นทุกข้อความก่อนนำไปใช้

```go
verifier, err := checkslip.NewWebhookVerifier(os.Getenv("CHECKSLIP_WEBHOOK_SECRET"))
if err != nil {
	log.Fatal(err)
}

http.HandleFunc("/hooks/checkslip", func(w http.ResponseWriter, r *http.Request) {
	ev, err := verifier.VerifyRequest(r)
	if err != nil {
		http.Error(w, "bad signature", http.StatusUnauthorized)
		return
	}
	if seenBefore(ev.ID) { // ข้อความเดียวกันอาจมาถึงมากกว่าหนึ่งครั้ง
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if ev.Type == checkslip.EventSlipChecked {
		data, err := ev.SlipChecked()
		if err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		markOrder(data.ClientReference, data.Outcome, data.Slip)
	}
	w.WriteHeader(http.StatusNoContent)
})
```

`VerifyRequest` อ่าน body ดิบเอง จึงห้าม parse JSON ก่อนเรียก ข้อความที่ timestamp ห่างจากนาฬิกาของคุณเกิน 5 นาทีจะถูกปฏิเสธ (เปลี่ยนได้ด้วย `SetTolerance`) คำตอบ 2xx ใดก็ได้ถือว่าส่งสำเร็จ คำตอบอื่นจะถูกส่งซ้ำ 8 ครั้งในราว 27.5 ชั่วโมง และคำตอบ `410 Gone` จะปิด endpoint

หลังเรียก `RotateSecret` secret เดิมยังเซ็นต่อไปอีก 24 ชั่วโมง และแต่ละข้อความมีลายเซ็นทั้งสองชุด ระหว่างเปลี่ยนให้ส่ง secret ทั้งสองตัว

```go
verifier, err := checkslip.NewWebhookVerifier(newSecret, oldSecret)
```

ถ้ามี body และ header อยู่แล้ว ใช้ `verifier.Verify(header, body)` หรือ `checkslip.VerifyWebhook(secret, header, body)` ได้เหมือนกัน

## การตั้งค่า

```go
client := checkslip.NewClient(apiKey,
	checkslip.WithTimeout(10*time.Second),          // ต่อหนึ่งครั้งที่ส่ง ค่าเริ่มต้น 30 วินาที
	checkslip.WithMaxRetries(3),                    // ค่าเริ่มต้น 2 ใส่ 0 เพื่อปิดการส่งซ้ำ
	checkslip.WithUserAgent("my-shop/1.4"),         // ต่อหน้า User-Agent ของ SDK
	checkslip.WithHTTPClient(httpClient),           // proxy, TLS หรือ tracing ของคุณเอง
	checkslip.WithBaseURL("http://127.0.0.1:8080"), // mock server ในเครื่อง
)
```

`WithBaseURL` รับ `https` ส่วน `http` รับเฉพาะที่อยู่ loopback เพื่อไม่ให้ API key วิ่งผ่านเครือข่ายแบบไม่เข้ารหัส ถ้าตั้งค่าผิด คำขอแรกจะคืน error `WithHeader` ใช้เพิ่ม header ให้คำขอเดียว เช่น `traceparent` ของ W3C

`Client` ใช้ร่วมกันหลาย goroutine ได้ สร้างครั้งเดียวแล้วใช้ซ้ำ

## เวอร์ชัน

SDK ใช้ [semantic versioning](https://semver.org) ก่อน v1.0.0 รุ่น minor อาจเปลี่ยน API ได้ ทุกการเปลี่ยนแปลงบันทึกไว้ใน [CHANGELOG.md](CHANGELOG.md)

## สัญญาอนุญาต

MIT ดู [LICENSE](LICENSE) หากพบปัญหาด้านความปลอดภัย ดู [SECURITY.md](SECURITY.md)
