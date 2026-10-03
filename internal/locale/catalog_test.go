package locale

import (
	"path/filepath"
	"testing"
)

func TestMergedFillsMissingKeysFromChinese(t *testing.T) {
	dir := filepath.Join("..", "..", "locales")
	c, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	zh := c.Merged("zh-CN")
	en := c.Merged("en")
	if zh["settings.title"] == "" || en["settings.title"] == "" {
		t.Fatalf("titles zh=%q en=%q", zh["settings.title"], en["settings.title"])
	}
	if len(zh) != len(en) {
		t.Fatalf("key count zh=%d en=%d", len(zh), len(en))
	}
	for k := range zh {
		if _, ok := en[k]; !ok {
			t.Fatalf("english missing %s", k)
		}
	}
	if c.Resolve("nope") != "zh-CN" {
		t.Fatalf("resolve %s", c.Resolve("nope"))
	}
}
