module github.com/router-for-me/CLIProxyAPI/v7/plugins/cpa-plugin-volcengine-asset

go 1.26.0

require (
	github.com/router-for-me/CLIProxyAPI/v7 v7.0.0
	github.com/sirupsen/logrus v1.9.3
	github.com/tidwall/gjson v1.18.0
	github.com/volcengine/volcengine-go-sdk v1.2.47
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	github.com/volcengine/volc-sdk-golang v1.0.23 // indirect
	golang.org/x/sys v0.47.0 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
)

replace github.com/router-for-me/CLIProxyAPI/v7 => ../..
