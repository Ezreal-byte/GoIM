package desktop

import (
	"embed"
	"image/png"
	"sync"

	"github.com/vivaui/viva/gfx"
)

//go:embed emoji/*.png
var emojiImages embed.FS

// Keep Unicode and stable picker identities so old messages and recent choices
// remain compatible with previous GoIM versions.
type emojiChoice struct {
	Name, Label, Value string
}

var emojiChoices = []emojiChoice{
	{"grin", "龇牙", "😀"},
	{"smile", "开心", "😃"},
	{"laugh", "大笑", "😄"},
	{"beaming", "得意", "😁"},
	{"sweat-smile", "尴尬", "😅"},
	{"joy", "笑哭", "😂"},
	{"slightly-smile", "微笑", "🙂"},
	{"wink", "眨眼", "😉"},
	{"blush", "害羞", "😊"},
	{"heart-eyes", "喜欢", "😍"},
	{"star-eyes", "崇拜", "🤩"},
	{"kiss", "飞吻", "😘"},
	{"cool", "酷", "😎"},
	{"thinking", "思考", "🤔"},
	{"neutral", "无语", "😐"},
	{"sleep", "困了", "😴"},
	{"cry", "委屈", "😢"},
	{"sob", "大哭", "😭"},
	{"angry", "生气", "😠"},
	{"mind-blown", "震惊", "🤯"},
	{"party", "庆祝", "🥳"},
	{"surprise", "惊讶", "😮"},
	{"tongue", "调皮", "😛"},
	{"squint", "偷笑", "😆"},
	{"plead", "可怜", "🥺"},
	{"woozy", "晕乎乎", "🥴"},
	{"confused", "疑惑", "😕"},
	{"frown", "不开心", "🙁"},
	{"worried", "担心", "😟"},
	{"disappointed", "失望", "😞"},
	{"pensive", "低落", "😔"},
	{"relieved", "松口气", "😌"},
	{"scream", "吓到了", "😱"},
	{"flushed", "脸红", "😳"},
	{"grimace", "咬牙", "😬"},
	{"cold", "冷汗", "😰"},
	{"sweat", "擦汗", "😓"},
	{"weary", "好累", "😩"},
	{"tired", "困倦", "😫"},
	{"yawn", "打哈欠", "🥱"},
	{"zipper", "闭嘴", "🤐"},
	{"shush", "嘘", "🤫"},
	{"smirk", "坏笑", "😏"},
	{"unamused", "无奈", "😒"},
	{"roll-eyes", "翻白眼", "🙄"},
	{"suspicious", "怀疑", "🤨"},
	{"nerd", "学霸", "🤓"},
	{"monocle", "仔细看看", "🧐"},
	{"pout", "气鼓鼓", "😡"},
	{"swear", "气坏了", "🤬"},
	{"huff", "哼", "😤"},
	{"hot", "好热", "🥵"},
	{"freeze", "好冷", "🥶"},
	{"sick", "戴口罩", "😷"},
	{"bandage", "受伤", "🤕"},
	{"sneeze", "打喷嚏", "🤧"},
	{"yum", "馋了", "😋"},
	{"tongue-wink", "鬼脸", "😜"},
	{"zany", "搞怪", "🤪"},
	{"halo", "乖巧", "😇"},
	{"hugs", "抱抱", "🤗"},
	{"love", "爱心脸", "🥰"},
	{"smile-tear", "含泪微笑", "🥲"},
	{"rofl", "笑倒", "🤣"},
	{"salute", "敬礼", "🫡"},
	{"hand-mouth", "捂嘴", "🤭"},
	{"cat", "猫猫", "😺"},
	{"heart", "爱心", "❤️"},
	{"rose", "玫瑰", "🌹"},
	{"thumb", "点赞", "👍"},
	{"clap", "鼓掌", "👏"},
	{"wave", "挥手", "👋"},
	{"pray", "合十", "🙏"},
	{"sparkle", "闪闪发光", "✨"},
	{"gift", "礼物", "🎁"},
	{"facepalm", "捂脸", "🤦"},
	{"joy-cat", "猫猫笑哭", "😹"},
	{"skull", "骷髅", "💀"},
	{"bomb", "炸弹", "💣"},
}

var emojiBitmaps = sync.OnceValue(func() map[string]gfx.Bitmap {
	images := make(map[string]gfx.Bitmap, len(emojiChoices))
	for index, choice := range emojiChoices {
		file, err := emojiImages.Open("emoji/" + choice.Name + ".png")
		if err != nil {
			continue
		}
		picture, err := png.Decode(file)
		file.Close()
		if err != nil {
			continue
		}
		bounds := picture.Bounds()
		width, height := bounds.Dx(), bounds.Dy()
		pixels := make([]byte, width*height*4)
		for y := range height {
			for x := range width {
				red, green, blue, alpha := picture.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				i := (y*width + x) * 4
				pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = byte(blue>>8), byte(green>>8), byte(red>>8), byte(alpha>>8)
			}
		}
		images[choice.Name] = gfx.Bitmap{Pix: pixels, Width: width, Height: height, Stride: width * 4, ID: 0x474F494D454D0000 | uint64(index+1), Version: 1}
	}
	return images
})
