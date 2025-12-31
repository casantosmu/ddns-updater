package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type Config struct {
	ZoneName   string
	RecordName string
	APIToken   string
}

type CloudflareResponse[T any] struct {
	Result  []T   `json:"result"`
	Success bool  `json:"success"`
	Errors  []any `json:"errors"`
}

type Zone struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type DNSRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Type    string `json:"type"`
}

type DNSRecordPayload struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	Proxied bool   `json:"proxied"`
}

var httpClient = &http.Client{
	Timeout: 10 * time.Second,
}

const cloudflareBaseURL = "https://api.cloudflare.com/client/v4"

func getEnvVars() (*Config, error) {
	cfg := &Config{
		ZoneName:   os.Getenv("ZONE_NAME"),
		RecordName: os.Getenv("RECORD_NAME"),
		APIToken:   os.Getenv("API_TOKEN"),
	}

	var missingVars []string
	if cfg.ZoneName == "" {
		missingVars = append(missingVars, "ZONE_NAME")
	}
	if cfg.RecordName == "" {
		missingVars = append(missingVars, "RECORD_NAME")
	}
	if cfg.APIToken == "" {
		missingVars = append(missingVars, "API_TOKEN")
	}

	if len(missingVars) > 0 {
		return nil, fmt.Errorf("missing environment variables: %s", strings.Join(missingVars, ", "))
	}

	return cfg, nil
}

func getPublicIP() (string, error) {
	resp, err := httpClient.Get("https://api.ipify.org")
	if err != nil {
		return "", fmt.Errorf("failed to fetch public IP: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to fetch public IP (status %d): %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	ip := strings.TrimSpace(string(body))

	log.Printf("[INFO] Public IP address: %s", ip)
	return ip, nil
}

func cfRequest(method, endpoint string, token string, bodyData interface{}) (*http.Response, error) {
	var bodyReader io.Reader

	if bodyData != nil {
		jsonData, err := json.Marshal(bodyData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequest(method, cloudflareBaseURL+endpoint, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("cloudflare API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return resp, nil
}

func getZoneID(zoneName, token string) (string, error) {
	resp, err := cfRequest("GET", "/zones?name="+zoneName, token, nil)
	if err != nil {
		return "", fmt.Errorf("failed to fetch zone ID: %w", err)
	}
	defer resp.Body.Close()

	var cfResp CloudflareResponse[Zone]
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return "", fmt.Errorf("failed to decode zone response: %w", err)
	}

	if len(cfResp.Result) == 0 {
		return "", fmt.Errorf("zone not found")
	}

	id := cfResp.Result[0].ID
	log.Printf("[INFO] Zone ID: %s", id)
	return id, nil
}

func getRecordData(zoneID, recordName, token string) (*DNSRecord, error) {
	endpoint := fmt.Sprintf("/zones/%s/dns_records?name=%s", zoneID, recordName)
	resp, err := cfRequest("GET", endpoint, token, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch record data: %w", err)
	}
	defer resp.Body.Close()

	var cfResp CloudflareResponse[DNSRecord]
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return nil, fmt.Errorf("failed to decode record response: %w", err)
	}

	if len(cfResp.Result) == 0 {
		return nil, nil
	}

	record := cfResp.Result[0]
	log.Printf("[INFO] Record found. ID: %s - Current IP: %s", record.ID, record.Content)
	return &record, nil
}

func createDNSRecord(zoneID, recordName, ip, token string) error {
	payload := DNSRecordPayload{
		Type:    "A",
		Name:    recordName,
		Content: ip,
		Proxied: false,
	}

	endpoint := fmt.Sprintf("/zones/%s/dns_records", zoneID)
	resp, err := cfRequest("POST", endpoint, token, payload)
	if err != nil {
		return fmt.Errorf("failed to create DNS record: %w", err)
	}
	defer resp.Body.Close()

	log.Println("[INFO] DNS record created successfully.")
	return nil
}

func updateDNSRecord(zoneID, recordName, recordID, ip, token string) error {
	payload := DNSRecordPayload{
		Type:    "A",
		Name:    recordName,
		Content: ip,
		Proxied: false,
	}

	endpoint := fmt.Sprintf("/zones/%s/dns_records/%s", zoneID, recordID)
	resp, err := cfRequest("PUT", endpoint, token, payload)
	if err != nil {
		return fmt.Errorf("failed to update DNS record: %w", err)
	}
	defer resp.Body.Close()

	log.Println("[INFO] DNS record updated successfully.")
	return nil
}

func main() {
	cfg, err := getEnvVars()
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	publicIP, err := getPublicIP()
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	zoneID, err := getZoneID(cfg.ZoneName, cfg.APIToken)
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	recordData, err := getRecordData(zoneID, cfg.RecordName, cfg.APIToken)
	if err != nil {
		log.Fatalf("[FATAL] %v", err)
	}

	if recordData == nil {
		log.Println("[INFO] Record does not exist. Creating...")
		if err := createDNSRecord(zoneID, cfg.RecordName, publicIP, cfg.APIToken); err != nil {
			log.Fatalf("[FATAL] %v", err)
		}
		return
	}
	if recordData.Content == publicIP {
		log.Printf("[INFO] IP not changed (%s).", publicIP)
		return
	}
	log.Printf("[INFO] IP changed (%s -> %s). Updating...", recordData.Content, publicIP)
	if err := updateDNSRecord(zoneID, cfg.RecordName, recordData.ID, publicIP, cfg.APIToken); err != nil {
		log.Fatalf("[FATAL] %v", err)
	}
}
