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
	"sort"
	"strings"
	"sync"
	"time"
)

// ایس ایم ایس کے نتائج کے لیے اسٹرکچر
type SMSResult struct {
	Range   string `json:"range"`
	Number  string `json:"number"`
	Time    string `json:"time"`
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
		// json.Number اس کو e+11 بننے سے روکے گا اور اصل ویلیو محفوظ رکھے گا
		Number json.Number `json:"Number"`
		Range  string      `json:"range"`
	} `json:"data"`
}

func main() {
	http.HandleFunc("/api/sms", handleSMS)
	http.HandleFunc("/api/numbers", handleNumbers)

	fmt.Println("سرور پورٹ 8080 پر چل رہا ہے...")
	http.ListenAndServe(":8080", nil)
}

// ------------------------------------------------------------------
// 1. ایس ایم ایس حاصل کرنے والا فنکشن (/api/sms)
// ------------------------------------------------------------------
func handleSMS(w http.ResponseWriter, r *http.Request) {
	// 1. سب سے پہلے رینجز لائیں
	ranges, rawBody, statusCode, err := fetchRanges()
	
	// اگر نیٹ ورک کا ایرر ہو یا پیچھے سرور نے 200 کی بجائے کوئی اور کوڈ (مثلاً 403, 419) دیا ہو
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if statusCode != 200 || len(ranges) == 0 {
		// پیچھے سے آنے والا را ڈیٹا (Raw Data) سیدھا کلائنٹ کو بھیج دیں
		w.WriteHeader(statusCode)
		w.Write(rawBody)
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var allSMS []SMSResult

	// ہر رینج کے نمبرز بیک گراؤنڈ میں نکالنے کے لیے
	for _, rng := range ranges {
		wg.Add(1)
		go func(rName string) {
			defer wg.Done()
			numbers := fetchNumbers(rName)

			var numWg sync.WaitGroup
			for _, num := range numbers {
				numWg.Add(1)
				go func(nName string) {
					defer numWg.Done()
					// اس نمبر کے سارے ایس ایم ایس نکالیں
					messages := fetchSMS(rName, nName)

					mu.Lock()
					allSMS = append(allSMS, messages...)
					mu.Unlock()
				}(num)
			}
			numWg.Wait()
		}(rng)
	}

	wg.Wait()

	// وقت کے حساب سے سورٹنگ (نئے میسج سب سے اوپر)
	sort.Slice(allSMS, func(i, j int) bool {
		return allSMS[i].Time > allSMS[j].Time
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "success",
		"total":  len(allSMS),
		"data":   allSMS,
	})
}

// ------------------------------------------------------------------
// 2. صاف نمبرز حاصل کرنے والا فنکشن (/api/numbers)
// ------------------------------------------------------------------
func handleNumbers(w http.ResponseWriter, r *http.Request) {
	currentTimestamp := time.Now().UnixMilli()
	apiURL := fmt.Sprintf("https://www.ivasms.com/portal/numbers?draw=1&columns%%5B0%%5D%%5Bdata%%5D=number_id&columns%%5B0%%5D%%5Bname%%5D=id&columns%%5B0%%5D%%5Borderable%%5D=false&columns%%5B1%%5D%%5Bdata%%5D=Number&columns%%5B2%%5D%%5Bdata%%5D=range&columns%%5B3%%5D%%5Bdata%%5D=A2P&columns%%5B4%%5D%%5Bdata%%5D=LimitA2P&columns%%5B5%%5D%%5Bdata%%5D=limit_cli_a2p&columns%%5B6%%5D%%5Bdata%%5D=limit_cli_did_a2p&columns%%5B7%%5D%%5Bdata%%5D=action&columns%%5B7%%5D%%5Bsearchable%%5D=false&columns%%5B7%%5D%%5Borderable%%5D=false&order%%5B0%%5D%%5Bcolumn%%5D=1&order%%5B0%%5D%%5Bdir%%5D=desc&start=0&length=1000&search%%5Bvalue%%5D=&_=%d", currentTimestamp)

	req, _ := http.NewRequest("GET", apiURL, nil)
	setHeaders(req)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)

	// اگر پیچھے سے رسپانس 200 نہ ہو، تو را ڈیٹا ریٹرن کر دیں
	if resp.StatusCode != 200 {
		w.WriteHeader(resp.StatusCode)
		w.Write(bodyBytes)
		return
	}

	var ivaResp IvaNumbersResponse
	if err := json.Unmarshal(bodyBytes, &ivaResp); err != nil {
		// اگر JSON پارس نہ ہو (یعنی کلاؤڈ فلیئر کا ایچ ٹی ایم ایل پیج آ گیا ہو)
		w.WriteHeader(http.StatusBadGateway)
		w.Write(bodyBytes)
		return
	}

	var cleanNumbers []CleanNumber

	for _, item := range ivaResp.Data {
		cleanNumbers = append(cleanNumbers, CleanNumber{
			Network: item.Range,
			Number:  item.Number.String(), // یہ سائنسی انداز کو ختم کر کے بالکل اصل نمبر دے گا
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"total":   len(cleanNumbers),
		"numbers": cleanNumbers,
	})
}

// ------------------------------------------------------------------
// ہیلپر فنکشنز (Helper Functions)
// ------------------------------------------------------------------

func fetchRanges() ([]string, []byte, int, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("from", StartDate)
	writer.WriteField("to", EndDate)
	writer.WriteField("_token", CSRFToken)
	writer.Close()

	req, _ := http.NewRequest("POST", "https://www.ivasms.com/portal/sms/received/getsms", body)
	setHeaders(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	// اگر رسپانس ٹھیک نہ ہو تو سیدھا ڈیٹا واپس بھیجیں
	if resp.StatusCode != 200 {
		return nil, bodyBytes, resp.StatusCode, nil
	}

	re := regexp.MustCompile(`toggleRange\('([^']+)'`)
	matches := re.FindAllStringSubmatch(string(bodyBytes), -1)

	var ranges []string
	for _, m := range matches {
		if len(m) > 1 {
			ranges = append(ranges, m[1])
		}
	}
	return ranges, bodyBytes, resp.StatusCode, nil
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

	bodyBytes, _ := io.ReadAll(resp.Body)
	
	re := regexp.MustCompile(`toggleNumIsIHj\('([^']+)'`)
	matches := re.FindAllStringSubmatch(string(bodyBytes), -1)

	var numbers []string
	for _, m := range matches {
		if len(m) > 1 {
			numbers = append(numbers, m[1])
		}
	}
	return numbers
}

func fetchSMS(rangeName, number string) []SMSResult {
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

	bodyBytes, _ := io.ReadAll(resp.Body)
	
	// یہ ریجیکس اب میسج کے ساتھ ساتھ اس کا ٹائم بھی نکالے گا
	re := regexp.MustCompile(`(?s)<div class="msg-text">(.*?)</div>.*?<td class="time-cell">(.*?)</td>`)
	matches := re.FindAllStringSubmatch(string(bodyBytes), -1)

	var messages []SMSResult
	for _, m := range matches {
		if len(m) > 2 {
			cleanMsg := strings.ReplaceAll(m[1], "&#039;", "'")
			cleanMsg = strings.TrimSpace(cleanMsg)
			timeStr := strings.TrimSpace(m[2])

			messages = append(messages, SMSResult{
				Range:   rangeName,
				Number:  number,
				Time:    timeStr,
				Message: cleanMsg,
			})
		}
	}
	return messages
}
