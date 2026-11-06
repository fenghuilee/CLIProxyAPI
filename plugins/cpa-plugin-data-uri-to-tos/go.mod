module github.com/router-for-me/CLIProxyAPI/v8/plugins/cpa-plugin-data-uri-to-tos

go 1.26.10

require (
	github.com/MoeForge/moego v0.1.0-alpha.1
	github.com/router-for-me/CLIProxyAPI/v8 v8.0.0
	github.com/volcengine/ve-tos-golang-sdk/v2 v2.7.8
	golang.org/x/sync v0.23.0
	gopkg.in/yaml.v3 v3.0.1
)

replace github.com/router-for-me/CLIProxyAPI/v8 => ../..

replace github.com/MoeForge/moego v0.1.0-alpha.1 => github.com/router-for-me/moego v0.1.2
