package desktop

import (
	"crypto/sha256"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"goim/internal/domain"
	"goim/internal/i18n"
)

//go:embed assets/localsend-black.png
var localSendBlack []byte

//go:embed assets/localsend-white.png
var localSendWhite []byte

var localSendMarks = [2]gfx.Bitmap{
	localSendBitmap("localsend-dark-ink", localSendBlack),
	localSendBitmap("localsend-light-ink", localSendWhite),
}

func localSendBitmap(key string, bytes []byte) gfx.Bitmap {
	decoded, ok := decodeShareQR(bytes, 224)
	if ok {
		decoded.ID = shareQRBitmapID(shareQRKey{fileID: key, digest: sha256.Sum256(bytes)})
		decoded.Version = 1
	}
	return decoded
}

func drawLocalSendMark(f ui.C, theme ui.Theme, size float32) {
	mark := localSendMarks[0]
	if panelDark(theme) {
		mark = localSendMarks[1]
	}
	ui.Image(f, mark).Size(size, size)
}

type localSendDraftFile struct {
	path, name, size string
	folder           bool
}

type localSendPanel struct {
	scope                                                           gfx.Scope
	jumpToSettings                                                  func()
	tab                                                             int
	selected, checked                                               map[string]bool
	offerID                                                         string
	draft                                                           []localSendDraftFile
	manual, manualHTTP, composingText, linkOptions, showQR, picking bool
	allowHTTP, linkHTTPS                                            bool
	linkURL, retry, recordDetail                                    string
	text, address, port, receivePIN, sendPIN, linkPIN               ui.Field
	portValue                                                       int
	qr                                                              gfx.Bitmap
}

func (v *localSendPanel) observe(state domain.LocalSendStatus) {
	if state.Offer.ID != v.offerID {
		v.offerID = state.Offer.ID
		v.checked = make(map[string]bool, len(state.Offer.Files))
		for _, file := range state.Offer.Files {
			v.checked[file.ID] = true
		}
		if v.offerID != "" {
			v.tab = 1
		}
	}
	if state.Link.RequestID != "" {
		v.tab = 1
	}
	if v.linkURL != state.Link.URL {
		v.linkURL = state.Link.URL
		v.showQR = false
		v.qr = localSendBitmap(state.Link.URL, state.Link.QR)
	}
}

func (p *Panels) localSendSettings(f ui.C, theme ui.Theme, state domain.State, submit func(domain.Command)) {
	cfg := state.Settings.LocalSend
	status := state.LocalSend
	v := &p.localSend
	if cfg.Port == 0 {
		cfg.Port = 53317
	}
	if cfg.Port != v.portValue {
		v.portValue = cfg.Port
		v.port.SetText(strconv.Itoa(cfg.Port))
	}
	panelSection(f, i18n.T("LocalSend 互通"), i18n.T("开启后，可在同一局域网和手机 LocalSend 双向传送文件。默认关闭。"), func() {
		label := i18n.T("启用 LocalSend")
		description := i18n.T("已关闭")
		if status.Active {
			description = i18n.T("运行中")
		}
		if status.Problem != "" {
			description = status.Problem
		}
		p.settingsRow(f, theme, label, description, func() {
			p.settingsControl(f, 84, func() {
				value := ui.State[bool](f, "localsend-enabled")
				*value = cfg.Enabled
				if ui.Switch(f, value, "").Hit(p.tree.ID(v.scope, "enabled"), label).Disabled(status.Unavailable).Changed() {
					next := cfg
					next.Enabled = *value
					submit(domain.Command{Kind: domain.LocalSendConfigure, LocalSend: domain.LocalSendRequest{Config: next}})
				}
			})
		})
		if !cfg.Enabled {
			return
		}
		p.settingsRow(f, theme, i18n.T("LocalSend 端口"), i18n.T("默认 TCP 端口 53317。发现使用 UDP 53317。"), func() {
			ui.Row(f).Items(ui.Center).Gap(8).Children(func() {
				field := ui.TextFieldID(f, p.tree.ID(v.scope, "port"), &v.port, i18n.T("LocalSend 端口"), "53317").Width(100)
				port, err := strconv.Atoi(strings.TrimSpace(v.port.Text()))
				valid := err == nil && port >= 1 && port <= 65535
				if namedButton(f, p.tree.ID(v.scope, "save-port"), i18n.T("保存")).Outline().Disabled(!valid).Clicked() || field.Submitted() && valid {
					next := cfg
					next.Port = port
					submit(domain.Command{Kind: domain.LocalSendConfigure, LocalSend: domain.LocalSendRequest{Config: next}})
				}
			})
		})
		p.settingsRow(f, theme, i18n.T("HTTP 明文接收"), i18n.T("关闭时使用 HTTPS。启用 HTTP 后，文件和 PIN 在网络上以明文传输。"), func() {
			value := ui.State[bool](f, "localsend-http")
			*value = cfg.HTTP
			if ui.Switch(f, value, "").Hit(p.tree.ID(v.scope, "receive-http"), i18n.T("HTTP 明文接收")).Changed() {
				next := cfg
				next.HTTP = *value
				submit(domain.Command{Kind: domain.LocalSendConfigure, LocalSend: domain.LocalSendRequest{Config: next}})
			}
		})
		pinDescription := i18n.T("4–8 位数字，仅在本次运行期间有效。留空保存可清除。")
		if status.HasPIN {
			pinDescription = i18n.T("接收 PIN 已设置。留空保存可清除。")
		}
		p.settingsRow(f, theme, i18n.T("LocalSend 接收 PIN"), pinDescription, func() {
			ui.Row(f).Items(ui.Center).Gap(8).Children(func() {
				field := ui.TextFieldID(f, p.tree.ID(v.scope, "receive-pin"), &v.receivePIN, i18n.T("LocalSend 接收 PIN"), "PIN").Width(100)
				if namedButton(f, p.tree.ID(v.scope, "save-pin"), i18n.T("保存")).Outline().Disabled(!status.Active).Clicked() || field.Submitted() && status.Active {
					password := v.receivePIN.Text()
					v.receivePIN.SetText("")
					submit(domain.Command{Kind: domain.LocalSendConfigure, Password: password, LocalSend: domain.LocalSendRequest{Config: cfg, SetPIN: true}})
				}
			})
		})
	})
}

func (p *Panels) drawLocalSend(f ui.C, theme ui.Theme, width float32, state domain.State, submit func(domain.Command)) {
	v := &p.localSend
	status := state.LocalSend
	ui.Column(f.Part("localsend-panel")).Fill().Pad(min(width/30, 26)).Gap(14).Children(func() {
		ui.Row(f).FillWidth().Gap(12).Items(ui.Center).Children(func() {
			drawPanelPageHeading(f, width, "LocalSend", i18n.T("与同一局域网的设备互传文件。"))
			if status.Active && namedButton(f, p.tree.ID(v.scope, "settings"), i18n.T("模块设置")).Outline().Clicked() {
				f.After(v.jumpToSettings)
			}
		})
		if !status.Active {
			ui.Column(f).Fill().Center().Gap(14).Children(func() {
				drawLocalSendMark(f, theme, 52)
				title := i18n.T("启用 LocalSend 兼容功能")
				body := i18n.T("开启后，即可与手机 LocalSend 互相发现并传送文件。")
				if state.Settings.LocalSend.Enabled && status.Problem != "" {
					title, body = i18n.T("LocalSend 服务未启动"), status.Problem
				}
				ui.Text(f, title).FontSize(textSize(f, 23)).Weight(gfx.WeightSemibold)
				ui.Wrap(f, body).MaxWidth(470).Muted()
				if namedButton(f, p.tree.ID(v.scope, "open-settings"), i18n.T("打开网络设置")).Clicked() {
					f.After(v.jumpToSettings)
				}
			})
			return
		}
		tabs := [...]int{0, 1, 2}
		labels := [...]string{i18n.T("发送"), i18n.T("接收"), i18n.T("记录")}
		ui.Tabs(f.Part("localsend-tabs"), &v.tab, tabs[:], func(i int) string { return labels[i] }).Line().Width(242)
		if status.Problem != "" {
			ui.Wrap(f, status.Problem).Muted()
		}
		ui.Divider(f).FillWidth()
		ui.Scroll(f.KeyInt(v.tab).Part("body")).Grow(1).MinHeight(0).FillWidth().Scrollbars().Children(func() {
			ui.Column(f).FillWidth().Gap(14).Children(func() {
				switch v.tab {
				case 0:
					p.drawLocalSendSend(f, theme, width, status, submit)
				case 1:
					p.drawLocalSendReceive(f, theme, width, status, state.Settings.DownloadDir, submit)
				case 2:
					p.drawLocalSendRecords(f, theme, width, submit)
				}
			})
		})
	})
}

func (p *Panels) drawLocalSendSend(f ui.C, theme ui.Theme, width float32, status domain.LocalSendStatus, submit func(domain.Command)) {
	v := &p.localSend
	ui.Row(f).FillWidth().Items(ui.Center).Gap(8).Children(func() {
		ui.Text(f, i18n.T("选择接收设备")).Grow(1).FontSize(textSize(f, 19)).Weight(gfx.WeightSemibold)
		if namedButton(f, p.tree.ID(v.scope, "manual"), i18n.T("按地址添加")).Outline().Clicked() {
			v.manual = !v.manual
		}
		if namedButton(f, p.tree.ID(v.scope, "refresh"), i18n.T("刷新")).Outline().Clicked() {
			submit(domain.Command{Kind: domain.LocalSendRefresh})
		}
	})
	if v.manual {
		ui.Column(f).FillWidth().Pad(12).Gap(8).Radius(radiusControl).Border(1, theme.Border).Children(func() {
			ui.Row(f).FillWidth().Items(ui.Center).Gap(8).Children(func() {
				ui.TextFieldID(f, p.tree.ID(v.scope, "device-address"), &v.address, i18n.T("设备 IP 地址"), "192.168.1.85:53317").Grow(1)
				if namedButton(f, p.tree.ID(v.scope, "add-device"), i18n.T("添加设备")).Disabled(strings.TrimSpace(v.address.Text()) == "").Clicked() {
					submit(domain.Command{Kind: domain.LocalSendAddDevice, Address: v.address.Text(), LocalSend: domain.LocalSendRequest{AllowHTTP: v.manualHTTP}})
				}
			})
			ui.Checkbox(f.Part("manual-http"), &v.manualHTTP, i18n.T("通过 HTTP（明文）连接"))
			ui.Wrap(f, i18n.T("证书变更时，请先在对方设备上核对地址，再手动添加。")).Muted()
		})
	}
	var ids []string
	cleartext := false
	if len(status.Devices) == 0 {
		ui.Column(f).FillWidth().Height(196).Center().Gap(10).Radius(radiusCard).Background(theme.Muted).Children(func() {
			drawLocalSendMark(f, theme, 38)
			ui.Text(f, i18n.T("暂未找到附近设备")).FontSize(textSize(f, 18)).Weight(gfx.WeightSemibold)
			ui.Wrap(f, i18n.T("让两台设备连接同一 Wi-Fi，并在手机上打开 LocalSend。")).Muted().MaxWidth(400)
		})
	}
	if v.selected == nil {
		v.selected = make(map[string]bool)
	}
	columns := 1
	if width >= 740 {
		columns = 2
	}
	for index := 0; index < len(status.Devices); index += columns {
		ui.Row(f.KeyInt(index)).FillWidth().Items(ui.Stretch).Gap(12).Children(func() {
			for _, device := range status.Devices[index:min(index+columns, len(status.Devices))] {
				picked := v.selected[device.ID]
				card := ui.Row(f.Key(device.ID)).Grow(1).MinWidth(0).MinHeight(68).Pad(12).Gap(10).Items(ui.Center).
					Radius(radiusCard).Border(1, theme.Border).Hover(ui.Look{}.Background(theme.Hover)).
					HitWith(p.tree.ID(v.scope, "device:"+device.ID), device.Name, gfx.HitOptions{Role: gfx.RoleButton, Selected: picked, Disabled: device.Changed})
				if picked {
					card = card.Background(panelAccentSurface(theme))
				}
				card.Children(func() {
					drawLocalSendMark(f, theme, 25)
					ui.Column(f).Grow(1).MinWidth(0).Gap(3).Children(func() {
						ui.Text(f, device.Name).Weight(gfx.WeightSemibold).Ellipsis()
						meta := device.Address
						if device.Model != "" {
							meta = device.Model + " · " + meta
						}
						if device.Plain {
							meta += " · HTTP"
						}
						if device.Changed {
							meta = i18n.T("设备证书已变更，请核对后重新添加。")
						}
						ui.Text(f, meta).Muted().Ellipsis()
					})
					if picked {
						ui.Text(f, "✓").Color(panelAccentText(theme))
					}
				})
				if card.Clicked() {
					picked = !picked
					v.selected[device.ID] = picked
				}
				if picked && !device.Changed {
					ids = append(ids, device.ID)
					cleartext = cleartext || device.Plain
				}
			}
			if columns == 2 && index+1 == len(status.Devices) {
				ui.Spacer(f)
			}
		})
	}

	ui.Row(f).FillWidth().Items(ui.Center).Gap(8).Children(func() {
		if namedButton(f, p.tree.ID(v.scope, "pick-files"), i18n.T("选择文件")).Outline().Disabled(v.picking).Clicked() {
			p.pickLocalSend(f, false)
		}
		if namedButton(f, p.tree.ID(v.scope, "pick-folder"), i18n.T("选择文件夹")).Outline().Disabled(v.picking).Clicked() {
			p.pickLocalSend(f, true)
		}
		if namedButton(f, p.tree.ID(v.scope, "write-text"), i18n.T("发送文字")).Outline().Clicked() {
			v.composingText = !v.composingText
		}
	})
	if v.composingText {
		ui.TextAreaID(f, p.tree.ID(v.scope, "message"), &v.text, i18n.T("要发送的文字"), i18n.T("输入文字")).FillWidth().Height(95)
	} else if len(v.draft) > 0 {
		p.localSendTable(f, theme, func() {
			for _, item := range v.draft {
				key := f.Key(item.path)
				p.localSendFileRow(key, theme, item.name, item.size, item.folder, func() {
					if namedButton(key, p.tree.ID(v.scope, "remove:"+item.path), i18n.T("移除")).Small().Outline().Clicked() {
						draft := make([]localSendDraftFile, 0, len(v.draft)-1)
						for _, other := range v.draft {
							if other.path != item.path {
								draft = append(draft, other)
							}
						}
						v.draft = draft
					}
				})
			}
		})
	}
	if cleartext {
		ui.Checkbox(f.Part("allow-http"), &v.allowHTTP, i18n.T("允许向所选设备明文发送文件和 PIN"))
	} else {
		v.allowHTTP = false
	}
	ui.Row(f).FillWidth().Items(ui.Center).Gap(8).Children(func() {
		ui.Text(f, i18n.T("已选设备")+" "+intString(len(ids))).Grow(1).Muted()
		if namedButton(f, p.tree.ID(v.scope, "send"), i18n.T("发送")).Disabled(len(ids) == 0 ||
			v.composingText && v.text.Text() == "" || !v.composingText && len(v.draft) == 0 || cleartext && !v.allowHTTP).Clicked() {
			c := domain.Command{Kind: domain.LocalSendSend, IDs: ids, LocalSend: domain.LocalSendRequest{AllowHTTP: v.allowHTTP}}
			if v.composingText {
				c.Text = v.text.Text()
			} else {
				c.Paths = v.paths()
			}
			submit(c)
			v.allowHTTP = false
			v.tab = 2
		}
	})
	p.drawLocalSendLink(f, theme, status.Link, false, submit)
}

func (p *Panels) pickLocalSend(f ui.C, folder bool) {
	v := &p.localSend
	v.picking = true
	canvas := f.Canvas()
	f.After(func() {
		defer func() { v.picking = false; canvas.RequestFrame() }()
		var paths []string
		var err error
		if folder {
			var path string
			path, err = app.OpenDirectory(app.OpenDirectoryOptions{Title: i18n.T("选择文件夹")})
			if err == nil && path != "" {
				paths = []string{path}
			}
		} else {
			paths, err = app.OpenFiles(app.OpenFileOptions{Title: i18n.T("选择文件"), AllowMultiple: true})
		}
		if err != nil {
			if !errors.Is(err, app.ErrCanceled) && p.toast != nil {
				p.toast("LocalSend", filePickerProblem(err), components.ToastError)
			}
			return
		}
		for _, path := range paths {
			known := false
			for _, old := range v.draft {
				known = known || old.path == path
			}
			if known {
				continue
			}
			size := i18n.T("文件夹")
			if !folder {
				info, err := os.Stat(path)
				if err != nil {
					continue
				}
				size = panelFormatBytes(info.Size())
			}
			v.draft = append(v.draft, localSendDraftFile{path: path, name: filepath.Base(path), size: size, folder: folder})
		}
		if len(paths) > 0 {
			v.composingText = false
		}
	})
}

func (v *localSendPanel) paths() []string {
	paths := make([]string, len(v.draft))
	for i, f := range v.draft {
		paths[i] = f.path
	}
	return paths
}

func (p *Panels) drawLocalSendReceive(f ui.C, theme ui.Theme, width float32, status domain.LocalSendStatus, dir string, submit func(domain.Command)) {
	v := &p.localSend
	ui.Column(f).FillWidth().Items(ui.Center).Gap(12).Children(func() {
		ui.Column(f.Part("receiver-card")).Width(min(700, width-2*min(width/30, 26))).MinWidth(0).
			Radius(radiusCard).Border(1, theme.Border).Pad(18).Gap(12).Children(func() {
			if status.Offer.ID == "" {
				ui.Text(f, i18n.T("等待附近设备发送")).FontSize(textSize(f, 19)).Weight(gfx.WeightSemibold)
				ui.Wrap(f, status.Alias+" · "+i18n.T("请在手机 LocalSend 中选择这台电脑。")).Muted()
				ui.Wrap(f, dir).Muted()
				return
			}
			offer := status.Offer
			ui.Wrap(f, offer.From+" · "+i18n.T("接收请求")).FontSize(textSize(f, 20)).Weight(gfx.WeightSemibold)
			c := domain.Command{Kind: domain.LocalSendAnswer, TransferID: offer.ID}
			if offer.IsText {
				ui.Wrap(f, offer.Text).FontSize(textSize(f, 15))
			} else {
				p.localSendTable(f, theme, func() {
					for _, file := range offer.Files {
						key := f.Key(file.ID)
						p.localSendFileRow(key, theme, file.Name, panelFormatBytes(file.Size), false, func() {
							picked := v.checked[file.ID]
							if ui.Checkbox(key.Part("accept"), &picked, "").Label(i18n.T("选择 ") + file.Name).Changed() {
								v.checked[file.ID] = picked
							}
						})
					}
				})
				for _, file := range offer.Files {
					if v.checked[file.ID] {
						c.IDs = append(c.IDs, file.ID)
					}
				}
			}
			ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
				ui.Spacer(f)
				if namedButton(f, p.tree.ID(v.scope, "reject"), i18n.T("拒绝")).Outline().Clicked() {
					submit(c)
				}
				label := i18n.T("接收所选")
				if offer.IsText {
					label = i18n.T("接收并复制")
				}
				if namedButton(f, p.tree.ID(v.scope, "accept"), label).Disabled(!offer.IsText && len(c.IDs) == 0).Clicked() {
					c.Bool = true
					submit(c)
					if offer.IsText {
						p.localSendCopy(f, offer.Text)
					}
				}
			})
		})
	})
	p.drawLocalSendLink(f, theme, status.Link, true, submit)
	if status.Link.RequestID != "" {
		ui.Column(f).FillWidth().Radius(radiusCard).Border(1, theme.Border).Pad(16).Gap(8).Children(func() {
			ui.Text(f, i18n.T("批准浏览器请求")).FontSize(textSize(f, 17)).Weight(gfx.WeightSemibold)
			ui.Wrap(f, status.Link.RequestFrom+" · "+i18n.T("浏览器请求下载文件")).Muted()
			ui.Row(f).FillWidth().Gap(8).Children(func() {
				c := domain.Command{Kind: domain.LocalSendAnswerLink, TransferID: status.Link.RequestID}
				if namedButton(f, p.tree.ID(v.scope, "deny-link"), i18n.T("拒绝")).Outline().Clicked() {
					submit(c)
				}
				if namedButton(f, p.tree.ID(v.scope, "approve-link"), i18n.T("批准")).Clicked() {
					c.Bool = true
					submit(c)
				}
			})
		})
	}
}

func (p *Panels) localSendTable(f ui.C, theme ui.Theme, rows func()) {
	ui.Column(f.Part("file-table")).FillWidth().Radius(radiusControl).Border(1, theme.Border).Children(func() {
		ui.Row(f).FillWidth().Height(32).PadX(12).Items(ui.Center).Background(theme.Muted).Children(func() {
			ui.Text(f, i18n.T("名称")).Grow(1).Muted()
			ui.Text(f, i18n.T("大小")).Width(72).Muted()
			ui.Text(f, i18n.T("操作")).Width(63).Muted()
		})
		rows()
	})
}

func (p *Panels) localSendFileRow(f ui.C, theme ui.Theme, name, size string, folder bool, actions func()) {
	ui.Divider(f).FillWidth()
	ui.Row(f).FillWidth().MinHeight(43).PadX(12).Gap(8).Items(ui.Center).Children(func() {
		drawFileIcon(f, theme, fileTypeIcon(domain.File{Name: name, Folder: folder}), 30, 19)
		ui.Text(f, name).Grow(1).MinWidth(0).Ellipsis()
		ui.Text(f, size).Width(72).Muted().Ellipsis()
		ui.Row(f).Width(60).Items(ui.Center).Children(actions)
	})
}

func (p *Panels) drawLocalSendRecords(f ui.C, theme ui.Theme, width float32, submit func(domain.Command)) {
	v := &p.localSend
	columns := measureTransferColumns(width)
	if width < 630 {
		columns.peer, columns.source, columns.status, columns.actions = 60, 55, 92, 72
	}
	found := false
	for i := range p.transferRows {
		if p.transferRows[i].transfer.LegacyTransport == domain.TransportLocalSend {
			found = true
			break
		}
	}
	if !found {
		panelEmpty(f, theme, i18n.T("暂无 LocalSend 记录"), i18n.T("发送和接收进度会显示在这里。"))
		return
	}
	for i := range p.transferRows {
		entry := &p.transferRows[i]
		if entry.transfer.LegacyTransport != domain.TransportLocalSend {
			continue
		}
		ui.Column(f.Key(entry.transfer.ID)).FillWidth().Children(func() {
			p.drawTransferRow(f, theme, columns, entry, submit)
			if len(entry.transfer.LocalSendItems) > 0 {
				if namedButton(f.Part("details"), p.tree.ID(v.scope, "details:"+entry.transfer.ID), i18n.T("文件清单")).Small().Outline().Clicked() {
					if v.recordDetail == entry.transfer.ID {
						v.recordDetail = ""
					} else {
						v.recordDetail = entry.transfer.ID
					}
				}
				if v.recordDetail == entry.transfer.ID {
					p.localSendTable(f, theme, func() {
						for _, item := range entry.transfer.LocalSendItems {
							key := f.Key(item.ID)
							p.localSendFileRow(key, theme, item.Name, panelFormatBytes(item.Size), false, func() {
								ui.Text(key, panelTransferStatus(item.Status, entry.transfer.Direction)).FontSize(textSize(key, 12)).Ellipsis()
							})
						}
					})
				}
			}
			if entry.transfer.LocalSendNeedsPIN || entry.transfer.ID == v.retry {
				ui.Row(f).FillWidth().PadX(14).Gap(8).Items(ui.Center).Children(func() {
					if v.retry != entry.transfer.ID {
						if namedButton(f, p.tree.ID(v.scope, "request-pin:"+entry.transfer.ID), i18n.T("输入对方 PIN")).Outline().Clicked() {
							v.retry = entry.transfer.ID
							v.sendPIN.SetText("")
						}
						return
					}
					ui.TextFieldID(f, p.tree.ID(v.scope, "send-pin"), &v.sendPIN, i18n.T("对方 PIN"), "PIN").Width(130)
					if namedButton(f, p.tree.ID(v.scope, "retry-with-pin"), i18n.T("继续发送")).Clicked() {
						submit(domain.Command{Kind: domain.RetryTransfer, TransferID: entry.transfer.ID, Password: v.sendPIN.Text()})
						v.retry = ""
						v.sendPIN.SetText("")
					}
				})
			}
		})
	}
}

func (p *Panels) drawLocalSendLink(f ui.C, theme ui.Theme, link domain.LocalSendLinkState, upload bool, submit func(domain.Command)) {
	v := &p.localSend
	title := i18n.T("创建下载链接")
	if upload {
		title = i18n.T("创建接收链接")
	}
	ui.Divider(f).FillWidth()
	if link.URL != "" {
		ui.Wrap(f, link.URL).Weight(gfx.WeightMedium)
		ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
			if namedButton(f, p.tree.ID(v.scope, "copy-link"), i18n.T("复制链接")).Outline().Clicked() {
				p.localSendCopy(f, link.URL)
			}
			if namedButton(f, p.tree.ID(v.scope, "show-qr"), i18n.T("二维码")).Outline().Clicked() {
				v.showQR = !v.showQR
			}
			if namedButton(f, p.tree.ID(v.scope, "stop-link"), i18n.T("关闭链接")).Outline().Clicked() {
				submit(domain.Command{Kind: domain.LocalSendCloseLink})
			}
		})
		if v.showQR && v.qr.Width > 0 {
			ui.Image(f, v.qr).Size(158, 158)
		}
		return
	}
	if namedButton(f, p.tree.ID(v.scope, "link-options"), title).Outline().Disabled(!upload && len(v.draft) == 0).Clicked() {
		v.linkOptions = !v.linkOptions
	}
	if !v.linkOptions {
		return
	}
	ui.Wrap(f, i18n.T("同一局域网的设备可用浏览器打开。链接最多保留 30 分钟，每次请求需要确认。")).Muted()
	ui.Checkbox(f.Part("link-https"), &v.linkHTTPS, i18n.T("使用 HTTPS（浏览器可能提示自签证书）"))
	if !v.linkHTTPS {
		ui.Wrap(f, i18n.T("此链接使用 HTTP，文件和 PIN 将明文传输。")).Color(theme.Danger)
	}
	ui.Row(f).FillWidth().Gap(8).Items(ui.Center).Children(func() {
		ui.TextFieldID(f, p.tree.ID(v.scope, "link-pin"), &v.linkPIN, i18n.T("链接 PIN"), i18n.T("可选 PIN")).Width(140)
		if namedButton(f, p.tree.ID(v.scope, "start-link"), i18n.T("生成局域网链接")).Disabled(!upload && len(v.draft) == 0).Clicked() {
			submit(domain.Command{Kind: domain.LocalSendLink, Paths: v.paths(), Password: v.linkPIN.Text(),
				LocalSend: domain.LocalSendRequest{Upload: upload, HTTPS: v.linkHTTPS}})
			v.linkPIN.SetText("")
			v.linkOptions = false
		}
	})
}

func (p *Panels) localSendCopy(f ui.C, value string) {
	f.After(func() {
		err := app.SetClipboardText(value)
		if p.toast == nil {
			return
		}
		if err != nil {
			p.toast(i18n.T("复制失败"), err.Error(), components.ToastError)
		} else {
			p.toast(i18n.T("已复制"), "", components.ToastSuccess)
		}
	})
}
