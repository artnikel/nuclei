// package license represents functions for license verification
package license

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/artnikel/nuclei/internal/constants"
)

// LicenseClient represents a client to check the license on the remote server
type LicenseClient struct {
	serverURL  string
	licenseKey string
	lastCheck  time.Time
	isValid    bool
}

// LicenseResponse describes the structure of the response from the licensing server
type LicenseResponse struct {
	Valid     bool   `json:"valid"`
	ExpiresAt string `json:"expires_at"`
	Message   string `json:"message"`
}

// NewLicenseClient creates a new instance of LicenseClient with the given server URL and license key
func NewLicenseClient(serverURL, licenseKey string) *LicenseClient {
	return &LicenseClient{
		serverURL:  serverURL,
		licenseKey: licenseKey,
	}
}

// CheckLicense checks the license on the remote server. Caches the result for 24 hours
func (lc *LicenseClient) CheckLicense() error {
	if time.Since(lc.lastCheck) < constants.DayTimeout && lc.isValid {
		return nil
	}

	client := &http.Client{Timeout: constants.TenSecTimeout}
	req, err := http.NewRequest(http.MethodGet, lc.serverURL+"/check-license", nil)
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

// IsValid returns a flag indicating the validity of the license after the last check
func (lc *LicenseClient) IsValid() bool {
	return lc.isValid
}
