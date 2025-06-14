package license

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/artnikel/nuclei/internal/constants"
)

type License struct {
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
	LastCheck time.Time `json:"last_check"`
	Active    bool      `json:"active"`
}

type LicenseClient struct {
	serverURL  string
	licenseKey string
	lastCheck  time.Time
	isValid    bool

	LicenseData License
}

func NewLicenseClient(serverURL, licenseKey string) *LicenseClient {
	return &LicenseClient{
		serverURL:  serverURL,
		licenseKey: licenseKey,
	}
}

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

	var lic License
	if err := json.NewDecoder(resp.Body).Decode(&lic); err != nil {
		return fmt.Errorf("failed to decode license response: %w", err)
	}

	if !lic.Active {
		lc.isValid = false
		return fmt.Errorf("license invalid: license is not active")
	}

	lc.isValid = true
	lc.lastCheck = time.Now()
	lc.LicenseData = lic

	return nil
}

func (lc *LicenseClient) IsValid() bool {
	return lc.isValid
}
