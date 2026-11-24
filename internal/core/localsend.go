package core

import (
	"errors"
	"strings"

	"goim/internal/domain"
	"goim/internal/localsend"
)

func (n *Node) applyLocalSend(cfg domain.LocalSendConfig) error {
	n.localSendMu.Lock()
	defer n.localSendMu.Unlock()
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return n.ctx.Err()
	}
	alias, network := n.data.State.Settings.Nickname, n.data.State.Settings.Network
	n.localSendProblem = ""
	n.mu.Unlock()
	if s := n.localSend.Swap(nil); s != nil {
		s.Close()
	}
	defer n.signal()
	if !cfg.Enabled || n.compatibilityDisabled {
		return nil
	}
	cfg.PIN = n.localSendPIN
	s, err := localsend.Start(localsend.Options{
		Context: n.ctx, Config: cfg, IdentityDir: n.dir, Alias: alias, Network: network,
		NoDiscovery: !n.discovery,
		DownloadDir: func() string {
			n.mu.Lock()
			defer n.mu.Unlock()
			return n.data.State.Settings.LocalSendReceiveDirectory()
		},
		AllowReceive: func() bool {
			n.mu.Lock()
			defer n.mu.Unlock()
			return !n.closing && !n.data.State.Locked && n.data.State.Settings.LocalSend.Enabled
		},
		Changed: n.signal, Persist: n.persistLocalSend,
	})
	if err != nil {
		n.mu.Lock()
		n.localSendProblem = "LocalSend 服务启动失败：" + err.Error()
		n.mu.Unlock()
		return err
	}
	n.localSend.Store(s)
	return nil
}

func (n *Node) stopLocalSend() {
	n.localSendMu.Lock()
	defer n.localSendMu.Unlock()
	if s := n.localSend.Swap(nil); s != nil {
		s.Close()
	}
}

func (n *Node) lockLocalSend() {
	if s := n.localSend.Load(); s != nil {
		s.PauseReceive()
	}
}

func (n *Node) persistLocalSend(row domain.Transfer) {
	_ = n.change(func(state *domain.State) {
		if i := transferIndex(state, row.ID); i >= 0 {
			state.Transfers[i] = row
		} else {
			state.Transfers = append(state.Transfers, row)
		}
	})
}

func (n *Node) addLocalSendSnapshot(state *domain.State) {
	status := domain.LocalSendStatus{Unavailable: n.compatibilityDisabled, Problem: n.localSendProblem}
	if s := n.localSend.Load(); s != nil {
		var live []domain.Transfer
		status, live = s.View()
		positions := make(map[string]int, len(live))
		for i, row := range state.Transfers {
			if row.LegacyTransport == domain.TransportLocalSend {
				positions[row.ID] = i
			}
		}
		for _, row := range live {
			if i, ok := positions[row.ID]; ok {
				state.Transfers[i] = row
			} else {
				state.Transfers = append(state.Transfers, row)
			}
		}
	}
	state.LocalSend = status
}

func (n *Node) configureLocalSend(c domain.Command) error {
	if n.compatibilityDisabled {
		return errors.New("此进程已关闭兼容服务")
	}
	cfg := c.LocalSend.Config
	if cfg.Port == 0 {
		cfg.Port = localsend.Port
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("端口应在 1 到 65535 之间")
	}
	if c.LocalSend.SetPIN {
		pin := c.Password
		if !validLocalSendPIN(pin) {
			return errors.New("PIN 需要 4 到 8 位数字")
		}
		n.localSendPIN = pin
	}
	n.mu.Lock()
	old := n.data.State.Settings.LocalSend
	n.mu.Unlock()
	if cfg != old {
		if err := n.change(func(state *domain.State) { state.Settings.LocalSend = cfg }); err != nil {
			return err
		}
	}
	if s := n.localSend.Load(); s != nil && old == cfg {
		if c.LocalSend.SetPIN {
			s.SetPIN(n.localSendPIN)
			n.signal()
		}
		return nil
	}
	return n.applyLocalSend(cfg)
}

func validLocalSendPIN(pin string) bool {
	return pin == "" || len(pin) >= 4 && len(pin) <= 8 && strings.Trim(pin, "0123456789") == ""
}

func (n *Node) executeLocalSend(c domain.Command) error {
	if c.Kind == domain.LocalSendConfigure {
		return n.configureLocalSend(c)
	}
	s := n.localSend.Load()
	if s == nil {
		return errors.New("请先启用 LocalSend 兼容功能")
	}
	switch c.Kind {
	case domain.LocalSendRefresh:
		s.Scan()
	case domain.LocalSendAddDevice:
		n.async(func() {
			if err := s.AddDevice(c.Address, c.LocalSend.AllowHTTP); err != nil {
				n.report(err)
			}
		})
	case domain.LocalSendSend:
		n.async(func() {
			if err := s.Send(c.IDs, c.Paths, c.Text, c.Password, c.LocalSend.AllowHTTP); err != nil {
				n.report(err)
			}
		})
	case domain.LocalSendAnswer:
		return s.Answer(c.TransferID, c.IDs, c.Bool)
	case domain.RetryTransfer:
		return s.Retry(c.TransferID, c.Password)
	case domain.AcceptTransfer:
		return errors.New("请在 LocalSend 的接收页面选择文件")
	case domain.CancelTransfer, domain.RejectTransfer:
		if !s.Cancel(c.TransferID) {
			return errors.New("LocalSend 任务已结束")
		}
	}
	return nil
}
