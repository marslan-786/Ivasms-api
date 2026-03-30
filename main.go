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

type CleanNumber struct {
	Network string `json:"network"`
	Number  string `json:"number"`
}

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

func handleSMS(w http.ResponseWriter, r *http.Request) {
	ranges, rawBody, statusCode, err := fetchRanges()
	
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	if statusCode != 200 || len(ranges) == 0 {
		w.WriteHeader(statusCode)
		w.Write(rawBody)
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	
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

	sort.Slice(allSMS, func(i, j int) bool {
		return allSMS[i][3] > allSMS[j][3]
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(allSMS)
}

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

func fetchRanges() ([]string, []byte, int, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("from", getStartDate())
	writer.WriteField("to", getEndDate())
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
	data.Set("start", getStartDate())
	data.Set("end", getEndDate())
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
	data.Set("start", getStartDate())
	data.Set("end", getEndDate())
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
	
	re := regexp.MustCompile(`(?s)<tr>\s*<td>(.*?)</td>\s*<td><div class="msg-text">(.*?)</div></td>\s*<td class="time-cell">(.*?)</td>`)
	matches := re.FindAllStringSubmatch(string(bodyBytes), -1)
	
	htmlTagRe := regexp.MustCompile(`<[^>]*>`)

	var messages [][]string
	for _, m := range matches {
		if len(m) > 3 {
			sender := htmlTagRe.ReplaceAllString(m[1], "")
			sender = strings.TrimSpace(sender)

			cleanMsg := strings.ReplaceAll(m[2], "&#039;", "'")
			cleanMsg = strings.ReplaceAll(cleanMsg, "&lt;", "<")
			cleanMsg = strings.ReplaceAll(cleanMsg, "&gt;", ">")
			cleanMsg = strings.TrimSpace(cleanMsg)

			timeStr := strings.TrimSpace(m[3])
			loc, _ := time.LoadLocation("Asia/Karachi")
			currentDate := time.Now().In(loc).Format("2006-01-02")
			fullTime := fmt.Sprintf("%s %s", currentDate, timeStr)

			row := []string{sender, number, cleanMsg, fullTime}
			messages = append(messages, row)
		}
	}
	return messages
}
