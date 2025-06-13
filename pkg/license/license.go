// package license represents functions for license verification
package license

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
type LicenseValidateResponse struct {
	Status string `json:"status"`
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

	u, err := url.Parse(lc.serverURL + "/validate")
	if err != nil {
		return fmt.Errorf("invalid license server URL: %w", err)
	}
	q := u.Query()
	q.Set("key", lc.licenseKey)
	u.RawQuery = q.Encode()

	client := &http.Client{Timeout: constants.TenSecTimeout}
	resp, err := client.Get(u.String())
	if err != nil {
		return fmt.Errorf("license check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("license server returned status: %d", resp.StatusCode)
	}

	var respData LicenseValidateResponse
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return fmt.Errorf("failed to decode license response: %w", err)
	}

	if respData.Status != "valid" {
		lc.isValid = false
		return fmt.Errorf("license invalid: status = %s", respData.Status)
	}

	lc.isValid = true
	lc.lastCheck = time.Now()

	return nil
}

// IsValid returns a flag indicating the validity of the license after the last check
func (lc *LicenseClient) IsValid() bool {
	return lc.isValid
}
