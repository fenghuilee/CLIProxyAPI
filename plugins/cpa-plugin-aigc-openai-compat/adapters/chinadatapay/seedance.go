// Package chinadatapay provides adapters for ChinaDataPay gateway models.
package chinadatapay

import (
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/adapters/volcengine"
	"github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-aigc-openai-compat/driver"
)

// ChinaDataPaySeedanceTasksURL is the ChinaDataPay Seedance gateway endpoint.
const ChinaDataPaySeedanceTasksURL = "https://tp-api.chinadatapay.com:8000/api/v3/contents/generations/tasks"

// NewChinaDataPaySeedanceAdapter creates a Seedance adapter configured for ChinaDataPay.
func NewChinaDataPaySeedanceAdapter() *volcengine.SeedanceAdapter {
	return volcengine.NewSeedanceAdapter(volcengine.SeedanceConfig{
		Provider:         "chinadatapay-seedance",
		ArtifactProvider: "cdp",
		DefaultEndpoint:  ChinaDataPaySeedanceTasksURL,
		SupportsCancel:   false,
		Prefixes: []string{
			"cdp/doubao-seedance",
			"chinadatapay/doubao-seedance",
		},
	})
}

func init() {
	driver.MustRegisterFactory(driver.Factory{
		Name: "chinadatapay-seedance",
		New:  func() driver.Adapter { return NewChinaDataPaySeedanceAdapter() },
	})
}
