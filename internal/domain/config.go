package domain

// Account stores the single local Wolt account used by the CLI.
type Account struct {
	Location      Location `json:"location,omitempty"`
	WToken        string   `json:"wtoken,omitempty"`
	WRefreshToken string   `json:"wrefresh_token,omitempty"`
	Cookies       []string `json:"cookies,omitempty"`
	WoltAddressID string   `json:"wolt_address_id,omitempty"`
}

// Profile stores legacy user location settings.
type Profile struct {
	Name          string   `json:"name"`
	IsDefault     bool     `json:"is_default"`
	Location      Location `json:"location"`
	WToken        string   `json:"wtoken,omitempty"`
	WRefreshToken string   `json:"wrefresh_token,omitempty"`
	Cookies       []string `json:"cookies,omitempty"`
	WoltAddressID string   `json:"wolt_address_id,omitempty"`
}

// Config stores all local profiles.
type Config struct {
	Account  Account   `json:"account,omitempty"`
	Profiles []Profile `json:"profiles,omitempty"`
}
