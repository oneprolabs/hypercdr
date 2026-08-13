package platform

// Edition identifies the product assembled around the shared HyperCDR core.
type Edition string

const (
	EditionCommunity  Edition = "community"
	EditionEnterprise Edition = "enterprise"
)

// Capability describes one edition-level product capability. Authorization is
// still enforced by the underlying API; capabilities are product discovery,
// not a security boundary.
type Capability struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Source  string `json:"source"`
}

// LicenseStatus is deliberately provider-neutral. The enterprise repository
// can later replace its development provider with signed-license validation.
type LicenseStatus struct {
	Mode   string `json:"mode"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// ProductInfo is returned by the public product-info endpoint.
type ProductInfo struct {
	Product      string                `json:"product"`
	Edition      Edition               `json:"edition"`
	Capabilities map[string]Capability `json:"capabilities"`
	License      LicenseStatus         `json:"license"`
}

// Options controls edition assembly without exposing internal server types.
type Options struct {
	Edition      Edition
	Capabilities map[string]Capability
	License      LicenseStatus
}

func CommunityOptions() Options {
	return Options{
		Edition: EditionCommunity,
		Capabilities: map[string]Capability{
			"coreDR":           {ID: "coreDR", Enabled: true, Source: "community"},
			"basicDiagnostics": {ID: "basicDiagnostics", Enabled: true, Source: "community"},
			"basicAudit":       {ID: "basicAudit", Enabled: true, Source: "community"},
		},
		License: LicenseStatus{Mode: "open-source", Status: "not-required", Detail: "Apache License 2.0"},
	}
}

func normalizeOptions(options Options) Options {
	if options.Edition == "" {
		options = CommunityOptions()
	}
	if options.Capabilities == nil {
		options.Capabilities = map[string]Capability{}
	}
	return options
}
