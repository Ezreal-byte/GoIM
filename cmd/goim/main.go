package main

import (
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"goim/internal/buildinfo"
	"goim/internal/core"
	"goim/internal/desktop"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	data := flag.String("data-dir", "", "独立数据目录（默认系统用户配置目录下 GoIM）")
	listen := flag.String("listen", "0.0.0.0:47842", "TCP 监听地址；独立实例可使用 127.0.0.1:0")
	name := flag.String("name", "", "首次或本次使用的昵称")
	peers := flag.String("connect", "", "启动后连接的 IP:端口，以逗号分隔")
	noDiscovery := flag.Bool("no-discovery", false, "关闭 UDP 自动发现（保留真实 TCP 通信）")
	noIPMSG := flag.Bool("no-ipmsg", false, "关闭旧版 IPMSG 兼容服务")
	noCompatibility := flag.Bool("no-compatibility", false, "本次运行不自动启动内网通和飞鸽／飞秋兼容服务")
	showVersion := flag.Bool("version", false, "显示版本")
	downloadDir := flag.String("download-dir", "", "显式覆盖文件保存目录，适用于独立测试实例")
	flag.Parse()
	build := buildinfo.Current()
	revision := build.Revision
	if revision == "" {
		revision = "working-tree"
	}
	version := fmt.Sprintf("%s (%s)", build.Version, revision)
	if *showVersion {
		fmt.Println("GoIM " + version)
		return nil
	}
	restart, err := restartArguments(flag.CommandLine)
	if err != nil {
		return err
	}
	n, err := core.New(core.Options{
		DataDir: *data, Listen: *listen, Name: *name,
		DisableDiscovery: *noDiscovery, DisableIPMSG: *noIPMSG,
		DisableCompatibility: *noCompatibility,
		Version:              version, DownloadDir: *downloadDir,
	})
	if err != nil {
		return err
	}
	defer n.Close()
	for _, address := range strings.Split(*peers, ",") {
		if strings.TrimSpace(address) != "" {
			n.Connect(address)
		}
	}
	return desktop.Run(n, restart)
}

// A replacement process can start in a different working directory. Preserve
// the meaning of GoIM's two directory flags when the installer relaunches it.
func restartArguments(flags *flag.FlagSet) ([]string, error) {
	args := make([]string, 0, flags.NFlag())
	var pathErr error
	flags.Visit(func(f *flag.Flag) {
		value := f.Value.String()
		if value != "" && (f.Name == "data-dir" || f.Name == "download-dir") {
			path, err := filepath.Abs(value)
			if err != nil {
				pathErr = fmt.Errorf("resolve --%s: %w", f.Name, err)
				return
			}
			value = path
		}
		args = append(args, "--"+f.Name+"="+value)
	})
	return args, pathErr
}
