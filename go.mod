module goim

go 1.26.0

require (
	github.com/ebitengine/purego v0.11.1
	github.com/godbus/dbus/v5 v5.1.0
	github.com/jezek/xgb v1.1.1
	github.com/kbinani/screenshot v0.0.0-20250624051815-089614a94018
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/vivaui/viva v0.6.6-0.20261118022843-059256b5ba10
	go.etcd.io/bbolt v1.5.0
	golang.org/x/sys v0.50.0
	golang.org/x/text v0.44.0
)

require (
	github.com/gen2brain/shm v0.1.0 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
)

// Pin the compatible Viva fork while native fixes, text scaling, and search highlighting reach upstream.
replace github.com/vivaui/viva => github.com/Ezreal-byte/viva v0.1.1-0.20261120105459-9fae79cadcf4
