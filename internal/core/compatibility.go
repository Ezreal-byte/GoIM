package core

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"

	"goim/internal/domain"
	ipmsgproto "goim/internal/ipmsg"
	"goim/internal/neiwangtong"
)

func (n *Node) initializeCompatibility() {
	if n.ipmsgOverride {
		n.ipmsgStatus = domain.CompatibilityStatus{Unavailable: true, Problem: "本次运行已通过 --no-ipmsg 禁用飞鸽／飞秋互通"}
	} else if cfg := n.data.State.Settings.IPMSG; cfg.Enabled {
		_ = n.applyIPMSG(cfg)
	}
	if cfg := n.data.State.Settings.Neiwangtong; cfg.Enabled {
		_ = n.applyNeiwangtong(cfg)
	}
}

func (n *Node) connectVia(transport, raw string) error {
	address := strings.TrimSpace(raw)
	if address == "" {
		return errors.New("请输入 IP 或 IP:端口")
	}
	switch transport {
	case domain.TransportGoIM:
		n.dialTo(address, "", false)
	case domain.TransportIPMSG:
		if addr, err := netip.ParseAddr(strings.Trim(address, "[]")); err == nil && addr.Unmap().Is4() {
			s := n.snapshot(false)
			address = netip.AddrPortFrom(addr.Unmap(), uint16(s.Settings.IPMSG.Port)).String()
		}
		n.async(func() {
			if err := n.connectLegacy(address); err != nil {
				n.report(err)
			}
		})
	case domain.TransportNeiwangtong:
		if addr, err := netip.ParseAddr(strings.Trim(address, "[]")); err == nil && addr.Unmap().Is4() {
			s := n.snapshot(false)
			address = netip.AddrPortFrom(addr.Unmap(), uint16(s.Settings.Neiwangtong.Port)).String()
		}
		n.async(func() {
			if err := n.connectNeiwangtong(address); err != nil {
				n.report(err)
			}
		})
	default:
		return errors.New("未知连接方式")
	}
	return nil
}

func (n *Node) setCompatibility(transport string, cfg domain.CompatibilityConfig) error {
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("端口须在 1 到 65535 之间")
	}
	n.compatMu.Lock()
	defer n.compatMu.Unlock()
	if transport == domain.TransportNeiwangtong {
		n.mu.Lock()
		if cfg == n.data.State.Settings.Neiwangtong && cfg.Enabled == (n.neiwangtong != nil) {
			n.neiwangtongStatus.Pending = nil
			n.neiwangtongStatus.Problem = ""
			n.mu.Unlock()
			n.signal()
			return nil
		}
		if n.neiwangtongTransfersActiveLocked() {
			n.neiwangtongStatus.Pending = &cfg
			n.neiwangtongStatus.Problem = ""
			n.mu.Unlock()
			n.signal()
			return nil
		}
		n.neiwangtongStatus.Pending = nil
		n.mu.Unlock()
		return n.applyNeiwangtong(cfg)
	}
	if transport != domain.TransportIPMSG {
		return errors.New("未知互通协议")
	}
	if n.ipmsgOverride {
		return errors.New("本次运行已通过 --no-ipmsg 禁用飞鸽／飞秋互通")
	}

	n.mu.Lock()
	if cfg == n.data.State.Settings.IPMSG && cfg.Enabled == (n.legacy != nil) {
		n.ipmsgStatus.Pending = nil
		n.ipmsgStatus.Problem = ""
		n.mu.Unlock()
		n.signal()
		return nil
	}
	if n.ipmsgTransfersActiveLocked() {
		n.ipmsgStatus.Pending = &cfg
		n.ipmsgStatus.Problem = ""
		n.mu.Unlock()
		n.signal()
		return nil
	}
	n.ipmsgStatus.Pending = nil
	n.mu.Unlock()
	return n.applyIPMSG(cfg)
}

func (n *Node) ipmsgTransfersActiveLocked() bool {
	for _, t := range n.data.State.Transfers {
		if t.LegacyTransport == domain.TransportIPMSG &&
			(t.Status == "transferring" || t.Status == "verifying" || n.transfers[t.ID] != nil) {
			return true
		}
	}
	return false
}

func (n *Node) neiwangtongTransfersActiveLocked() bool {
	for _, t := range n.data.State.Transfers {
		if t.LegacyTransport == domain.TransportNeiwangtong &&
			(t.Status == "transferring" || t.Status == "verifying" || n.transfers[t.ID] != nil) {
			return true
		}
	}
	return false
}

func (n *Node) kickCompatibility() {
	n.mu.Lock()
	if n.closing || n.compatScheduled {
		n.mu.Unlock()
		return
	}
	hasPendingIPMSG := n.ipmsgStatus.Pending != nil && !n.ipmsgTransfersActiveLocked()
	hasPendingNWT := n.neiwangtongStatus.Pending != nil && !n.neiwangtongTransfersActiveLocked()
	if !hasPendingIPMSG && !hasPendingNWT {
		n.mu.Unlock()
		return
	}
	n.compatScheduled = true
	n.mu.Unlock()
	n.async(func() {
		n.compatMu.Lock()
		defer n.compatMu.Unlock()
		n.mu.Lock()
		n.compatScheduled = false
		if n.closing {
			n.mu.Unlock()
			return
		}
		var applyIP *domain.CompatibilityConfig
		if n.ipmsgStatus.Pending != nil && !n.ipmsgTransfersActiveLocked() {
			cfg := *n.ipmsgStatus.Pending
			n.ipmsgStatus.Pending = nil
			applyIP = &cfg
		}
		var applyNWT *domain.CompatibilityConfig
		if n.neiwangtongStatus.Pending != nil && !n.neiwangtongTransfersActiveLocked() {
			cfg := *n.neiwangtongStatus.Pending
			n.neiwangtongStatus.Pending = nil
			applyNWT = &cfg
		}
		n.mu.Unlock()
		if applyIP != nil {
			if err := n.applyIPMSG(*applyIP); err != nil {
				n.report(err)
			}
		}
		if applyNWT != nil {
			if err := n.applyNeiwangtong(*applyNWT); err != nil {
				n.report(err)
			}
		}
	})
}

// applyIPMSG opens both replacement sockets before touching the active service or
// persisting the change. The caller serializes adapter changes.
func (n *Node) applyIPMSG(cfg domain.CompatibilityConfig) error {
	var service *ipmsgproto.Service
	live := new(atomic.Bool)
	if cfg.Enabled {
		port := strconv.Itoa(cfg.Port)
		if _, goPort, _ := net.SplitHostPort(n.listener.Addr().String()); port == goPort || cfg.Port == discoveryPort {
			return n.compatFailure(fmt.Errorf("端口 %d 已用于 GoIM 通信", cfg.Port))
		}
		var err error
		service, err = n.newIPMSG(cfg.Port, live)
		if err != nil {
			return n.compatFailure(err)
		}
	}
	n.mu.Lock()
	if n.ipmsgTransfersActiveLocked() {
		n.ipmsgStatus.Pending = &cfg
		n.mu.Unlock()
		if service != nil {
			service.Close()
		}
		n.signal()
		return nil
	}
	oldSettings := n.data.State.Settings.IPMSG
	n.data.State.Settings.IPMSG = cfg
	if err := saveStore(n.db, &n.data); err != nil {
		n.data.State.Settings.IPMSG = oldSettings
		n.mu.Unlock()
		if service != nil {
			service.Close()
		}
		return n.compatFailure(err)
	}
	old := n.legacy
	if n.legacyLive != nil {
		n.legacyLive.Store(false)
	}
	n.legacy, n.legacyLive = service, live
	n.ipmsgStatus = domain.CompatibilityStatus{Active: service != nil}
	for i := range n.data.State.Peers {
		if n.data.State.Peers[i].Transport == domain.TransportIPMSG {
			n.data.State.Peers[i].Online = false
		}
	}
	n.data.State.Revision++
	n.data.State.Error = ""
	broadcast := n.discovery && !n.data.State.Settings.DisableAutoDiscovery
	live.Store(service != nil)
	n.mu.Unlock()
	n.signal()
	if old != nil {
		old.Close()
	}
	if service != nil {
		service.SetBroadcastEnabled(broadcast)
	}
	return nil
}

func (n *Node) compatFailure(err error) error {
	err = fmt.Errorf("飞鸽／飞秋互通: %w", err)
	n.mu.Lock()
	n.ipmsgStatus.Problem = err.Error()
	n.mu.Unlock()
	n.signal()
	return err
}

func (n *Node) applyNeiwangtong(cfg domain.CompatibilityConfig) error {
	var service *neiwangtong.Service
	live := new(atomic.Bool)
	if cfg.Enabled {
		port := strconv.Itoa(cfg.Port)
		if _, goPort, _ := net.SplitHostPort(n.listener.Addr().String()); port == goPort || cfg.Port == discoveryPort {
			return n.compatNwtFailure(fmt.Errorf("端口 %d 已用于 GoIM 通信", cfg.Port))
		}
		var err error
		service, err = n.newNeiwangtong(cfg.Port, live)
		if err != nil {
			return n.compatNwtFailure(err)
		}
	}
	n.mu.Lock()
	if n.neiwangtongTransfersActiveLocked() {
		n.neiwangtongStatus.Pending = &cfg
		n.mu.Unlock()
		if service != nil {
			service.Close()
		}
		n.signal()
		return nil
	}
	oldSettings := n.data.State.Settings.Neiwangtong
	n.data.State.Settings.Neiwangtong = cfg
	if err := saveStore(n.db, &n.data); err != nil {
		n.data.State.Settings.Neiwangtong = oldSettings
		n.mu.Unlock()
		if service != nil {
			service.Close()
		}
		return n.compatNwtFailure(err)
	}
	old := n.neiwangtong
	if n.neiwangtongLive != nil {
		n.neiwangtongLive.Store(false)
	}
	n.neiwangtong, n.neiwangtongLive = service, live
	n.neiwangtongStatus = domain.CompatibilityStatus{Active: service != nil}
	for i := range n.data.State.Peers {
		if n.data.State.Peers[i].Transport == domain.TransportNeiwangtong {
			n.data.State.Peers[i].Online = false
		}
	}
	n.data.State.Revision++
	n.data.State.Error = ""
	broadcast := n.discovery && !n.data.State.Settings.DisableAutoDiscovery
	live.Store(service != nil)
	n.mu.Unlock()
	n.signal()
	if old != nil {
		old.Close()
	}
	if service != nil {
		service.SetBroadcastEnabled(broadcast)
	}
	return nil
}

func (n *Node) compatNwtFailure(err error) error {
	err = fmt.Errorf("内网通互通: %w", err)
	n.mu.Lock()
	n.neiwangtongStatus.Problem = err.Error()
	n.mu.Unlock()
	n.signal()
	return err
}

func (n *Node) hideUnusedDirectConversations() {
	s := &n.data.State
	used := make(map[string]bool, len(s.Conversations))
	for _, m := range s.Messages {
		used[m.ConversationID] = true
	}
	for _, f := range s.Files {
		used[f.ConversationID] = true
	}
	for _, t := range s.Transfers {
		used[t.ConversationID] = true
	}
	for i := range s.Conversations {
		c := &s.Conversations[i]
		if c.Kind == "direct" && !c.Opened && !used[c.ID] && c.Draft == "" &&
			c.Preview == "" && !c.Pinned && !c.Muted && c.Unread == 0 {
			c.Hidden = true
		}
	}
}
