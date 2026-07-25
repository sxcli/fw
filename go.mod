module sxcli.dev/fw

go 1.26.4

require golang.org/x/sys v0.47.0

require (
	github.com/goccy/go-yaml v1.19.2 // indirect
	sxcli.dev/rules v0.0.0 // indirect
)

require sxcli.dev/conf v0.1.1

replace sxcli.dev/conf => ../sxcli-conf

replace sxcli.dev/rules => ../sxcli-rules
