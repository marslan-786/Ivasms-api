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

// صاف نمبرز کے نتائج کے لیے اسٹرکچر
type CleanNumber struct {
	Network string `json:"network"`
	Number  string `json:"number"`
}

// ivaSMS سے آنے والے کچے ڈیٹا کا اسٹرکچر
type IvaNumbersResponse struct {
	Data []struct {
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
	ranges, rawBody, statusCode, err := fetchRanges()
	
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// اگر کوئی رینج نہیں ملی یا سرور نے ایرر دیا ہے، تو ہوبہو را ڈیٹا (Raw Data) دکھا دیں
	if statusCode != 200 || len(ranges) == 0 {
		w.WriteHeader(statusCode)
		w.Write(rawBody)
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	
	// اس کو make سے انیشلائز کیا ہے تاکہ خالی ہونے کی صورت میں null کی بجائے [] ریٹرن ہو
	allSMS := make([][]string, 0)

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
	// انڈیکس 3 پر ہمارا وقت (Time) محفوظ ہے
	sort.Slice(allSMS, func(i, j int) bool {
		return allSMS[i][3] > allSMS[j][3]
	})

	w.Header().Set("Content-Type", "application/json")
	// آپ کی ڈیمانڈ کے مطابق بالکل Array of Arrays والا فارمیٹ
	json.NewEncoder(w).Encode(allSMS)
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

	if resp.StatusCode != 200 {
		w.WriteHeader(resp.StatusCode)
		w.Write(bodyBytes)
		return
	}

	var ivaResp IvaNumbersResponse
	if err := json.Unmarshal(bodyBytes, &ivaResp); err != nil {
		w.WriteHeader(http.StatusBadGateway)
		w.Write(bodyBytes)
		return
	}

	var cleanNumbers []CleanNumber

	for _, item := range ivaResp.Data {
		cleanNumbers = append(cleanNumbers, CleanNumber{
			Network: item.Range,
			Number:  item.Number.String(),
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
	
	// یہاں ڈائنیمک نام کا حل کیا گیا ہے (toggleNum کے بعد کچھ بھی ہو اسے پکڑ لے گا)
	re := regexp.MustCompile(`toggleNum[a-zA-Z0-9_]+\('([^']+)'`)
	matches := re.FindAllStringSubmatch(string(bodyBytes), -1)

	var numbers []string
	for _, m := range matches {
		if len(m) > 1 {
			numbers = append(numbers, m[1])
		}
	}
	return numbers
}

func fetchSMS(rangeName, number string) [][]string {
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
	
	// بھیجنے والا، میسج اور وقت نکالنے کے لیے ریجیکس
	re := regexp.MustCompile(`(?s)<tr>\s*<td>(.*?)</td>\s*<td><div class="msg-text">(.*?)</div></td>\s*<td class="time-cell">(.*?)</td>`)
	matches := re.FindAllStringSubmatch(string(bodyBytes), -1)
	
	// ایچ ٹی ایم ایل ٹیگز صاف کرنے کے لیے
	htmlTagRe := regexp.MustCompile(`<[^>]*>`)

	var messages [][]string
	for _, m := range matches {
		if len(m) > 3 {
			// سینڈر کے ٹیگز صاف کرنا
			sender := htmlTagRe.ReplaceAllString(m[1], "")
			sender = strings.TrimSpace(sender)

			// میسج صاف کرنا
			cleanMsg := strings.ReplaceAll(m[2], "&#039;", "'")
			cleanMsg = strings.ReplaceAll(cleanMsg, "&lt;", "<")
			cleanMsg = strings.ReplaceAll(cleanMsg, "&gt;", ">")
			cleanMsg = strings.TrimSpace(cleanMsg)

			// وقت کو آپ کے فارمیٹ (YYYY-MM-DD HH:MM:SS) میں لانا
			timeStr := strings.TrimSpace(m[3])
			currentDate := time.Now().Format("2006-01-02")
			fullTime := fmt.Sprintf("%s %s", currentDate, timeStr)

			row := []string{sender, number, cleanMsg, fullTime}
			messages = append(messages, row)
		}
	}
	return messages
}
