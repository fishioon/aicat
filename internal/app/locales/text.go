package locales

import (
	"encoding/json"
	"sync"

	"github.com/egoist/mygo/i18n"
)

// Text localizes presentation state kept in the source language, such as
// progress labels and notices. Request/response payloads do not pass through it.
// Static UI messages use the generated catalog accessors directly.
func Text(source string) string {
	indexOnce.Do(func() {
		data, err := files.ReadFile("zh-Hans.json")
		if err != nil {
			panic(err)
		}
		var messages map[string]string
		if err := json.Unmarshal(data, &messages); err != nil {
			panic(err)
		}
		bySource = make(map[string]string, len(messages))
		for key, value := range messages {
			if key != "@@locale" {
				bySource[value] = key
			}
		}
	})
	if key, ok := bySource[source]; ok {
		return Catalog.Format(i18n.Locale(), key, nil)
	}
	return source
}

var indexOnce sync.Once
var bySource map[string]string
