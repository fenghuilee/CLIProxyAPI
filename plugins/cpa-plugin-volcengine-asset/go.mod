module github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-volcengine-asset

go 1.26.10

require (
	github.com/MoeForge/moego v0.1.0-alpha.1
	github.com/router-for-me/CLIProxyAPI/v8 v8.0.0
	github.com/volcengine/volcengine-go-sdk v1.2.47
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/volcengine/volc-sdk-golang v1.0.23 // indirect
	gopkg.in/yaml.v2 v2.2.8 // indirect
)

replace github.com/router-for-me/CLIProxyAPI/v8 => ../..

replace github.com/MoeForge/moego v0.1.0-alpha.1 => github.com/router-for-me/moego v0.1.2
