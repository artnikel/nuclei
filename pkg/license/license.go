package license

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type LicenseClient struct {
	serverURL  string
	licenseKey string
	lastCheck  time.Time
	isValid    bool
}

type LicenseResponse struct {
	Valid     bool   `json:"valid"`
	ExpiresAt string `json:"expires_at"`
	Message   string `json:"message"`
}

func NewLicenseClient(serverURL, licenseKey string) *LicenseClient {
	return &LicenseClient{
		serverURL:  serverURL,
		licenseKey: licenseKey,
	}
}

func (lc *LicenseClient) CheckLicense() error {
	if time.Since(lc.lastCheck) < 24*time.Hour && lc.isValid {
		return nil
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", lc.serverURL+"/check-license", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+lc.licenseKey)
	req.Header.Set("User-Agent", "NucleiScanner/3.0")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("license check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("license server returned status: %d", resp.StatusCode)
	}

	var licenseResp LicenseResponse
	if err := json.NewDecoder(resp.Body).Decode(&licenseResp); err != nil {
		return fmt.Errorf("failed to decode license response: %w", err)
	}

	lc.isValid = licenseResp.Valid
	lc.lastCheck = time.Now()

	if !licenseResp.Valid {
		return fmt.Errorf("license is invalid: %s", licenseResp.Message)
	}

	return nil
}

func (lc *LicenseClient) IsValid() bool {
	return lc.isValid
}
