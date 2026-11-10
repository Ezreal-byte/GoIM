// Package i18n translates GoIM's UI chrome. User content and wire values
// remain in their original language.
package i18n

import (
	"strings"
	"sync/atomic"
)

const Chinese = "zh-CN"
const English = "en"

var english atomic.Bool

func Normalize(language string) string {
	if strings.EqualFold(language, English) {
		return English
	}
	return Chinese
}

func SetLanguage(language string) { english.Store(Normalize(language) == English) }

func Language() string {
	if english.Load() {
		return English
	}
	return Chinese
}

func T(label string) string {
	if english.Load() {
		if translated, ok := en[label]; ok {
			return translated
		}
	}
	return label
}

func Labels(labels []string) []string {
	if !english.Load() {
		return labels
	}
	translated := make([]string, len(labels))
	for i, label := range labels {
		translated[i] = T(label)
	}
	return translated
}
