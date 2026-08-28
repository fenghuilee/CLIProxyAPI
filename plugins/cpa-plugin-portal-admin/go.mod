module github.com/router-for-me/CLIProxyAPI/v7/plugins/cpa-plugin-portal-admin

go 1.26.0

require (
	github.com/router-for-me/CLIProxyAPI/v7 v7.0.0
	github.com/shopspring/decimal v1.4.0
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/driver/mysql v1.5.7
	gorm.io/gorm v1.25.12
)

require (
	github.com/go-sql-driver/mysql v1.7.0 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/jinzhu/now v1.1.5 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace github.com/router-for-me/CLIProxyAPI/v7 => ../../
