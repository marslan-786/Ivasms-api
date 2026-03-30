package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ڈپلیکیٹ ایس ایم ایس کو فلٹر کرنے کے لیے گلوبل میپ
var seenSMS sync.Map

// ایس ایم ایس کے نتائج کے لیے اسٹرکچر
type SMSResult struct {
	Range   string `json:"range"`
	Number  string `json:"number"`
	Message string `json:"message"`
}

// صاف نمبرز کے نتائج کے لیے اسٹرکچر
type CleanNumber struct {
	Network string `json:"network"`
	Number  string `json:"number"`
}

// ivaSMS سے آنے والے کچے ڈیٹا کا اسٹرکچر
type IvaNumbersResponse struct {
	Data []struct {
		Number interface{} `json:"Number"`
		Range  string      `json:"range"`
	} `json:"data"`
}

func main() {
	// اپڈیٹ شدہ اینڈ پوائنٹس
	http.HandleFunc("/api/sms", handleSMS)
	http.HandleFunc("/api/numbers", handleNumbers)

	fmt.Println("سرور پورٹ 8080 پر چل رہا ہے...")
	http.ListenAndServe(":8080", nil)
}

// ------------------------------------------------------------------
// 1. ایس ایم ایس حاصل کرنے والا فنکشن (/api/sms)
// ------------------------------------------------------------------
func handleSMS(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	ranges := fetchRanges()

	var wg sync.WaitGroup
	var mu sync.Mutex
	var allNewSMS []SMSResult

	// ہر رینج کے نمبرز بیک گراؤنڈ میں نکالنے کے لیے
	for _, rng := range ranges {
		wg.Add(1)
		go func(rName string) {
			defer wg.Done()
			numbers := fetchNumbers(rName)

			var numWg sync.WaitGroup
			// ہر نمبر کے ایس ایم ایس بیک گراؤنڈ میں نکالنے کے لیے
			for _, num := range numbers {
				numWg.Add(1)
				go func(nName string) {
					defer numWg.Done()
					messages := fetchSMS(rName, nName)

					// ڈپلیکیٹ چیکنگ
					for _, msg := range messages {
						// اگر یہ میسج پہلے نہیں دیکھا، تو اسے شامل کریں
						if _, exists := seenSMS.LoadOrStore(msg, true); !exists {
							mu.Lock()
							allNewSMS = append(allNewSMS, SMSResult{
								Range:   rName,
								Number:  nName,
								Message: msg,
							})
							mu.Unlock()
						}
					}
				}(num)
			}
			numWg.Wait()
		}(rng)
	}

	wg.Wait()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "success",
		"time_ms":  time.Since(start).Milliseconds(),
		"new_data": allNewSMS,
	})
}

// ------------------------------------------------------------------
// 2. صاف نمبرز حاصل کرنے والا فنکشن (/api/numbers)
// ------------------------------------------------------------------
func handleNumbers(w http.ResponseWriter, r *http.Request) {
	// موجودہ وقت کا ٹائم سٹیمپ ملی سیکنڈز میں حاصل کریں
	currentTimestamp := time.Now().UnixMilli()

	// پورا URL بالکل آپ کی ریکویسٹ کے مطابق، بشمول تمام پیرامیٹرز
	// نوٹ: Go میں '%' کو فارمیٹ کرنے کے لیے '%%' لکھنا پڑتا ہے۔
	apiURL := fmt.Sprintf("https://www.ivasms.com/portal/numbers?draw=1&columns%%5B0%%5D%%5Bdata%%5D=number_id&columns%%5B0%%5D%%5Bname%%5D=id&columns%%5B0%%5D%%5Borderable%%5D=false&columns%%5B1%%5D%%5Bdata%%5D=Number&columns%%5B2%%5D%%5Bdata%%5D=range&columns%%5B3%%5D%%5Bdata%%5D=A2P&columns%%5B4%%5D%%5Bdata%%5D=LimitA2P&columns%%5B5%%5D%%5Bdata%%5D=limit_cli_a2p&columns%%5B6%%5D%%5Bdata%%5D=limit_cli_did_a2p&columns%%5B7%%5D%%5Bdata%%5D=action&columns%%5B7%%5D%%5Bsearchable%%5D=false&columns%%5B7%%5D%%5Borderable%%5D=false&order%%5B0%%5D%%5Bcolumn%%5D=1&order%%5B0%%5D%%5Bdir%%5D=desc&start=0&length=1000&search%%5Bvalue%%5D=&_=%d", currentTimestamp)

	req, _ := http.NewRequest("GET", apiURL, nil)
	setHeaders(req)
	// اس ریکویسٹ کے لیے مخصوص ہیڈر
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	
	w.Header().Set("Content-Type", "application/json")

	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "message": "نیٹ ورک کا مسئلہ"})
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var ivaResp IvaNumbersResponse
	if err := json.Unmarshal(bodyBytes, &ivaResp); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "message": "ڈیٹا پارس کرنے میں مسئلہ"})
		return
	}

	var cleanNumbers []CleanNumber

	// سارا کچرا صاف کر کے صرف رینج اور نمبر کو ایک لسٹ میں ڈالنا
	for _, item := range ivaResp.Data {
		numStr := fmt.Sprintf("%v", item.Number) // نمبر کو اسٹرنگ میں تبدیل کرنا
		cleanNumbers = append(cleanNumbers, CleanNumber{
			Network: item.Range,
			Number:  numStr,
		})
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"total":   len(cleanNumbers),
		"numbers": cleanNumbers,
	})
}


// ------------------------------------------------------------------
// ہیلپر فنکشنز (Helper Functions)
// ------------------------------------------------------------------

func fetchRanges() []string {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("from", StartDate)
	writer.WriteField("to", EndDate)
	writer.WriteField("_token", CSRFToken)
	writer.Close()

	req, _ := http.NewRequest("POST", "https://www.ivasms.com/portal/sms/received/getsms", body)
	setHeaders(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	
	re := regexp.MustCompile(`toggleRange\('([^']+)'`)
	matches := re.FindAllStringSubmatch(string(respBody), -1)

	var ranges []string
	for _, m := range matches {
		if len(m) > 1 {
			ranges = append(ranges, m[1])
		}
	}
	return ranges
}

func fetchNumbers(rangeName string) []string {
	data := url.Values{}
	data.Set("_token", CSRFToken)
	data.Set("start", StartDate)
	data.Set("end", EndDate)
	data.Set("range", rangeName)

	req, _ := http.NewRequest("POST", "https://www.ivasms.com/portal/sms/received/getsms/number", strings.NewReader(data.Encode()))
	setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	
	re := regexp.MustCompile(`toggleNumIsIHj\('([^']+)'`)
	matches := re.FindAllStringSubmatch(string(respBody), -1)

	var numbers []string
	for _, m := range matches {
		if len(m) > 1 {
			numbers = append(numbers, m[1])
		}
	}
	return numbers
}

func fetchSMS(rangeName, number string) []string {
	data := url.Values{}
	data.Set("_token", CSRFToken)
	data.Set("start", StartDate)
	data.Set("end", EndDate)
	data.Set("Number", number)
	data.Set("Range", rangeName)

	req, _ := http.NewRequest("POST", "https://www.ivasms.com/portal/sms/received/getsms/number/sms", strings.NewReader(data.Encode()))
	setHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=UTF-8")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	
	re := regexp.MustCompile(`<div class="msg-text">([^<]+)</div>`)
	matches := re.FindAllStringSubmatch(string(respBody), -1)

	var messages []string
	for _, m := range matches {
		if len(m) > 1 {
			cleanMsg := strings.ReplaceAll(m[1], "&#039;", "'")
			cleanMsg = strings.TrimSpace(cleanMsg)
			messages = append(messages, cleanMsg)
		}
	}
	return messages
}
