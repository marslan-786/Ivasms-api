package main

import (
	"net/http"
	"time"
)

const (
	CSRFToken = "4IhFspJNDLLUcj4kM5lW6nXr7FYtKeLZiq9QnGaU"

	Cookies = "cf_clearance=2uhbGeLDu1bbnJdHFGgzkr6qbGVCkw740_ng66kmQG0-1768791936-1.2.1.1-rhMpi5_5RtWtoLwP5vY.7fH9X9XRRY0t2PIqtC9nzANt7mwVId8Ai9U3cRt9JJZNxJ8TcHJXn22.b5nSowfsxJ6J_qcjv5bLgnTGnOxDrQRCiu0rNIW3cgGLZw3dOCCF1exwlhPzeR97ztVEKawWXO5Z7v4MwBu2ERBoMuznwBpX3dunPw0KbhLEqr_QoV6VvXAVPs1IDTbwjcJWH.L1dMG2d4h06y9ZKBFq7EnTlFo; _fbp=fb.1.1768792105881.346405700682289015; _ga=GA1.2.1307093810.1768792131; XSRF-TOKEN=eyJpdiI6InkvcU9iU2JRVWRZVFZ3Q1J1ckJWOHc9PSIsInZhbHVlIjoiTHZyRnF3Qy85dlFDaGhwN04rQm1NU3kzU2hBdGc3OHRNSW9pdmk1YzBDRVhyUWs3QU1QeVQ2N1pCTTRVWkxXU2xNeVBDTFoxb0pPUThQbzNNMStpOEV0RUFOWXl6S1crZGtjT3hZMVQNXJ6K0VVcG9kQzlkVCtXbHJJRldiTG4iLCJtYWMiOiJiNjJkMWM0NTcxODM4ZjExNmZiZDg3MDk5MDQwMjRiZTNiYjdhOTVlYzBiMTE1MTIyN2JjYjViMzE5ZTAyOTA5IiwidGFnIjoiIn0%3D; ivas_sms_session=eyJpdiI6IkdmWDY0YmYvVWJGWVJicXBZQU1tc2c9PSIsInZhbHVlIjoiNWJjejdscERBV3pQa2d0RFBzVkw4QVgrNHAwSE5pQ1dCRURQcXN2NnllRU84elpvNTQ1VmE1aldJN2pBQ0dBVHJUVlNhY1Y3TEE3QW1HRXRpZEdoaG5JQzk2b2R1anRRNGlqSko5WFhLblUvb2pETXd5ellqMnhjVWxwOXdlVC8iLCJtYWMiOiIwZTNjZmQwNWExNzRmYjM2ZjA4MzJlZDUzMDI0NWE4OWQ2MWJkNDM4ZDlkYTZkNmNmNGViNmFjNmJlMzYwOWIxIiwidGFnIjoiIn0%3D"

	UserAgent = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Mobile Safari/537.36"
)

func getStartDate() string {
	loc, err := time.LoadLocation("Asia/Karachi")
	now := time.Now()
	if err == nil {
		now = now.In(loc)
	}
	return now.AddDate(0, 0, -1).Format("2006-01-02")
}

func getEndDate() string {
	loc, err := time.LoadLocation("Asia/Karachi")
	now := time.Now()
	if err == nil {
		now = now.In(loc)
	}
	return now.AddDate(0, 0, 1).Format("2006-01-02")
}

func setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Cookie", Cookies)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("X-CSRF-TOKEN", CSRFToken)
	req.Header.Set("Accept", "text/html, */*; q=0.01")
}
