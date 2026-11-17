package neiwangtong

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Stock 3.4.3055 sends its built-in set as numeric feihu image references,
// starting at 1251. Render the same meaning using GoIM's own Unicode artwork;
// no proprietary GIF assets are bundled with the adapter.
var stockEmoticons = [...]string{
	"🙂", "😆", "😄", "😛", "😁", "😢", "😊", "😫", "😓", "😭", "😴", "💪",
	"😎", "❓", "😵", "👋", "😂", "👏", "😌", "[抽烟]", "😣", "🙁", "😞", "😤",
	"😠", "😡", "[衰]", "🥺", "😮", "🤐", "😒", "🤑", "😬", "🥶", "🤮", "👎",
	"🔎", "😍", "[抠鼻子]", "🤔", "😏", "🧐", "😿", "🤝", "👌", "🙌", "👍", "🍚",
	"🎂", "🍷", "🏀", "🎁", "✉️", "☕", "💡", "☎️", "📱", "🎵", "💡", "🕒",
	"☀️", "🌙", "🌧️", "⚡", "🌈", "❄️", "☂️", "🚗", "🍎", "🍉", "🍌", "🍓",
	"🍇", "🍰", "🥤", "🍬", "🍦", "🧃", "👩", "👨", "⚽", "🏸", "🎉", "💩",
}

func parseStockEmoticon(value string) (string, bool) {
	id, err := strconv.ParseUint(value, 10, 32)
	if err != nil || id == 0 || strings.Contains(value, "|") {
		return "", false
	}
	if id >= 1251 && id < 1251+uint64(len(stockEmoticons)) {
		return stockEmoticons[id-1251], true
	}
	return "[内网通表情 " + strconv.FormatUint(id, 10) + "]", true
}

func nativeTextParts(text string) []MsgDataTxt {
	parts := make([]MsgDataTxt, 0, 4)
	start := 0
	for at := 0; at < len(text); {
		index, length := -1, 0
		for i, value := range stockEmoticons {
			if !strings.HasPrefix(value, "[") && len(value) > length && strings.HasPrefix(text[at:], value) {
				index, length = i, len(value)
			}
		}
		if index < 0 {
			_, size := utf8.DecodeRuneInString(text[at:])
			at += size
			continue
		}
		if at > start {
			parts = append(parts, MsgDataTxt{Txt: &MsgTxt{T: "normal", V: text[start:at]}})
		}
		parts = append(parts, MsgDataTxt{Img: &MsgTxt{T: "feihu", V: strconv.Itoa(1251 + index)}})
		at += length
		start = at
		if len(parts) >= 62 {
			// Preserve the complete text even for messages with many emoji.
			break
		}
	}
	if start < len(text) || len(parts) == 0 {
		parts = append(parts, MsgDataTxt{Txt: &MsgTxt{T: "normal", V: text[start:]}})
	}
	return parts
}
