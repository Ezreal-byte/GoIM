module goim

go 1.26

require (
	github.com/jezek/xgb v1.1.1
	github.com/kbinani/screenshot v0.0.0-20250624051815-089614a94018
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/vivaui/viva v0.0.0
	go.etcd.io/bbolt v1.5.0
	golang.org/x/sys v0.45.0
	golang.org/x/text v0.36.0
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/gen2brain/shm v0.1.0 // indirect
	github.com/godbus/dbus/v5 v5.1.0 // indirect
	github.com/lxn/win v0.0.0-20210218163916-a377121e959e // indirect
)

replace github.com/vivaui/viva => ../viva
