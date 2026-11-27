// Package buildinfo 保存构建期注入的版本信息。
package buildinfo

// 这些变量可通过 -ldflags "-X ..." 覆盖。
var (
	Version = "0.1.0"
	Commit  = "dev"
	Date    = "unknown"
)
