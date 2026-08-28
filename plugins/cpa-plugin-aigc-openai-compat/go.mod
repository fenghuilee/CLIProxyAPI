module github.com/router-for-me/CLIProxyAPI/v7/plugins/cpa-plugin-aigc-openai-compat

go 1.26.0

require (
	github.com/router-for-me/CLIProxyAPI/v7 v7.0.0
	github.com/tidwall/gjson v1.18.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/kr/text v0.2.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace github.com/router-for-me/CLIProxyAPI/v7 => ../..
