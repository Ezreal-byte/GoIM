package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"goim/internal/core"
	"goim/internal/desktop"
)

var version = "0.1.0-dev"
var commit = "working-tree"

func main() {
	data := flag.String("data-dir", "", "独立数据目录（默认系统用户配置目录下 GoIM）")
	listen := flag.String("listen", "0.0.0.0:47842", "TCP 监听地址；独立实例可使用 127.0.0.1:0")
	name := flag.String("name", "", "首次或本次使用的昵称")
	peers := flag.String("connect", "", "启动后连接的 IP:端口，以逗号分隔")
	noDiscovery := flag.Bool("no-discovery", false, "关闭 UDP 自动发现（保留真实 TCP 通信）")
	showVersion := flag.Bool("version", false, "显示版本")
	flag.Parse()
	if *showVersion {
		fmt.Printf("GoIM %s (%s)\n", version, commit)
		return
	}
	n, err := core.New(core.Options{DataDir: *data, Listen: *listen, Name: *name, DisableDiscovery: *noDiscovery, Version: version + " (" + commit + ")"})
	if err != nil {
		log.Fatal(err)
	}
	defer n.Close()
	for _, address := range strings.Split(*peers, ",") {
		if strings.TrimSpace(address) != "" {
			n.Connect(address)
		}
	}
	if err = desktop.Run(n); err != nil {
		log.Print(err)
	}
}
