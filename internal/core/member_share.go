package core

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strconv"
	"strings"

	"goim/internal/domain"
)

type memberProject struct {
	Key       string
	Name      string `json:",omitempty"`
	Size      int64  `json:",omitempty"`
	Entries   int    `json:",omitempty"`
	Protected bool   `json:",omitempty"`
}

type memberCatalog struct {
	Port     uint16
	Projects []memberProject
}

// A catalog link expires along with the private browser share it refers to.
func memberProjectKey(token string) string {
	digest := sha256.Sum256([]byte("goim-member-share:" + token))
	return hex.EncodeToString(digest[:16])
}

// Caller holds n.mu so files, access settings and revision describe one catalog.
func (n *Node) shareCatalog() envelope {
	s := &n.data.State
	e := envelope{Type: "catalog", CatalogRevision: s.Revision}
	_, port, _ := net.SplitHostPort(n.webAddress)
	number, _ := strconv.ParseUint(port, 10, 16)
	for _, f := range s.Files {
		if !f.Shared || f.SenderID != s.Self.ID {
			continue
		}
		record := n.data.BrowserShares[f.ID]
		if !f.ShareFolder && !record.Password.set() {
			e.Files = append(e.Files, publicFile(f))
			continue
		}
		if number == 0 || record.Token == "" {
			continue
		}
		project := memberProject{Key: memberProjectKey(record.Token), Protected: record.Password.set()}
		if !project.Protected {
			project.Name = f.Name
			project.Entries = len(record.Entries)
			for _, entry := range record.Entries {
				project.Size += entry.Size
			}
		}
		if e.MemberCatalog == nil {
			e.MemberCatalog = &memberCatalog{Port: uint16(number)}
		}
		e.MemberCatalog.Projects = append(e.MemberCatalog.Projects, project)
	}
	return e
}

func (p *persisted) updateMemberProjects(peer string, incoming *memberCatalog) {
	delete(p.MemberProjects, peer)
	if incoming == nil || incoming.Port == 0 {
		return
	}
	valid := memberCatalog{Port: incoming.Port}
	seen := make(map[string]bool)
	for _, item := range incoming.Projects {
		key, err := hex.DecodeString(item.Key)
		if err != nil || len(key) != 16 || item.Key != strings.ToLower(item.Key) || seen[item.Key] {
			continue
		}
		if item.Protected {
			item = memberProject{Key: item.Key, Protected: true}
		} else if !validFileName(item.Name) || item.Size < 0 || item.Entries < 0 {
			continue
		}
		valid.Projects = append(valid.Projects, item)
		seen[item.Key] = true
	}
	if len(valid.Projects) > 0 {
		if p.MemberProjects == nil {
			p.MemberProjects = make(map[string]memberCatalog)
		}
		p.MemberProjects[peer] = valid
	}
}

func (n *Node) attachMemberProjects(s *domain.State) {
	for _, peer := range s.Peers {
		catalog, ok := n.data.MemberProjects[peer.ID]
		if !ok {
			continue
		}
		// Only construct browser URLs from the authenticated direct connection.
		var base string
		if pc := n.connections[peer.ID]; pc != nil && peer.Online {
			if host, _, err := net.SplitHostPort(pc.peer.Address); err == nil {
				base = "http://" + net.JoinHostPort(host, strconv.Itoa(int(catalog.Port))) + "/m/"
			}
		}
		for _, project := range catalog.Projects {
			f := domain.File{
				ID: "member:" + peer.ID + ":" + project.Key, SenderID: peer.ID, Shared: true,
				Name: project.Name, Size: project.Size, ShareEntryCount: project.Entries,
				ShareFolder: !project.Protected, ShareProtected: project.Protected,
			}
			if project.Protected {
				f.Name = "受密码保护的共享"
			}
			if base != "" {
				f.MemberURL = base + project.Key
			}
			s.Files = append(s.Files, f)
		}
	}
}

func (n *Node) openMemberProject(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/m/")
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || len(key) != 32 {
		http.NotFound(w, r)
		return
	}
	n.mu.Lock()
	token := ""
	for id, record := range n.data.BrowserShares {
		if memberProjectKey(record.Token) != key {
			continue
		}
		f := n.data.State.File(id)
		if f.Shared && f.SenderID == n.data.State.Self.ID && (f.ShareFolder || record.Password.set()) {
			token = record.Token
		}
		break
	}
	n.mu.Unlock()
	if token == "" {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/s/"+token, http.StatusFound)
}
