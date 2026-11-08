package ipmsg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	defaultPort = 2425

	commandEntry    = 0x00000001
	commandExit     = 0x00000002
	commandAnswer   = 0x00000003
	commandAbsence  = 0x00000004
	commandSend     = 0x00000020
	commandReceived = 0x00000021
	commandGetFile  = 0x00000060
	commandRelease  = 0x00000061

	optionAbsence   = 0x00000100
	optionSendCheck = 0x00000100
	optionFile      = 0x00200000
	optionUTF8      = 0x00800000
	optionCapUTF8   = 0x01000000
	optionBroadcast = 0x00000400

	fileRegular = 0x00000001
)

type packet struct {
	Version    int
	Number     uint64
	Sender     string
	Host       string
	Command    uint32
	Extra      string
	rawVersion string
}

func parsePacket(data []byte) (packet, error) {
	trimmed := bytes.TrimRight(data, "\x00\r\n")
	parts := bytes.SplitN(trimmed, []byte{':'}, 6)
	if len(parts) != 5 && len(parts) != 6 {
		return packet{}, fmt.Errorf("IPMSG packet has %d fields", len(parts))
	}
	rawVersion := string(parts[0])
	versionText := rawVersion
	if strings.HasPrefix(rawVersion, "1_lbt") && len(rawVersion) <= 512 {
		versionText = "1"
	}
	version, err := strconv.Atoi(versionText)
	if err != nil || version < 1 {
		return packet{}, fmt.Errorf("invalid IPMSG version")
	}
	number, err := strconv.ParseUint(string(parts[1]), 10, 64)
	if err != nil {
		return packet{}, fmt.Errorf("invalid IPMSG packet number")
	}
	command, err := parseCommand(string(parts[4]))
	if err != nil {
		return packet{}, err
	}
	extra := ""
	if len(parts) == 6 {
		extra = string(parts[5])
	}
	return packet{
		Version: version, Number: number,
		Sender: decodeLegacyString(parts[2]), Host: decodeLegacyString(parts[3]),
		Command: command, Extra: extra, rawVersion: string(parts[0]),
	}, nil
}

func parseCommand(value string) (uint32, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("invalid IPMSG command")
	}
	// IP Messenger writes the packet command in decimal form (for example,
	// SENDMSG is "32"), while the command constants in protocol headers are
	// hexadecimal. Accept an explicit hexadecimal form as a compatibility
	// fallback for uncommon clients, but never interpret the standard decimal
	// packet as hexadecimal.
	explicitHex := strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X")
	if explicitHex || strings.ContainsAny(strings.ToLower(value), "abcdef") {
		number, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X"), 16, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid IPMSG command")
		}
		return uint32(number), nil
	}
	number, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid IPMSG command")
	}
	return uint32(number), nil
}

func (p packet) baseCommand() uint32    { return p.Command & 0xff }
func (p packet) has(option uint32) bool { return p.Command&option != 0 }

func packetString(number uint64, sender, host string, command uint32, extra string) []byte {
	base := fmt.Sprintf("1:%d:%s:%s:%d:", number, headerField(sender), headerField(host), command)
	return []byte(base + extra + "\x00")
}

func headerField(value string) string {
	value = strings.ReplaceAll(value, ":", "；")
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func peerID(host, address string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(host) + "\x00" + address))
	return "ipmsg-" + hex.EncodeToString(sum[:8])
}

func decodeMessage(extra string, utf8Flag bool) (string, string) {
	message, attachments, _ := strings.Cut(extra, "\x00")
	if utf8Flag && utf8.ValidString(message) {
		return message, attachments
	}
	return decodeLegacyString([]byte(message)), attachments
}

func decodeLegacyString(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	if utf8.Valid(data) {
		return string(data)
	}
	candidates := []string{string(data)}
	for _, decoder := range []interface{ Bytes([]byte) ([]byte, error) }{
		simplifiedchinese.GB18030.NewDecoder(),
		japanese.ShiftJIS.NewDecoder(),
	} {
		if decoded, err := decoder.Bytes(data); err == nil && utf8.Valid(decoded) {
			candidates = append(candidates, string(decoded))
		}
	}
	best, bestScore := candidates[0], legacyTextScore(candidates[0])
	for _, candidate := range candidates[1:] {
		if score := legacyTextScore(candidate); score < bestScore {
			best, bestScore = candidate, score
		}
	}
	return best
}

func legacyTextScore(value string) int {
	score := 0
	for _, r := range value {
		switch {
		case r == utf8.RuneError:
			score += 100
		case r == 0:
			score += 50
		case unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t':
			score += 20
		}
	}
	return score
}
