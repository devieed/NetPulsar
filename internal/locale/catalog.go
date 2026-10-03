package locale

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var idPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,40}$`)

// Pack is one language. Strings is the key-value map used by the page.
type Pack struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Strings map[string]string `json:"strings"`
}

// Info is a language listed in settings.
type Info struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Catalog struct {
	Dir      string
	embedded fs.FS
}

func ResolveDir() string {
	cwd, _ := os.Getwd()
	exeDir := cwd
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		exeDir = filepath.Dir(exe)
		if isGoRun(exe) {
			if dir := filepath.Join(cwd, "locales"); hasPack(dir) {
				return dir
			}
		}
	}
	if dir := filepath.Join(exeDir, "locales"); hasPack(dir) {
		return dir
	}
	if dir := filepath.Join(cwd, "locales"); hasPack(dir) {
		return dir
	}
	if writable(exeDir) {
		return filepath.Join(exeDir, "locales")
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(cwd, "locales")
	}
	return filepath.Join(cfg, "NetPulsar", "locales")
}

func Open(dir string, embedded fs.FS) (*Catalog, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	c := &Catalog{Dir: dir, embedded: embedded}
	if err := c.Ensure(); err != nil {
		return nil, err
	}
	return c, nil
}

// Ensure copies bundled languages that are not on disk yet. Files the user already edited are left alone.
func (c *Catalog) Ensure() error {
	if c.embedded == nil {
		return nil
	}
	entries, err := fs.ReadDir(c.embedded, ".")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		dst := filepath.Join(c.Dir, e.Name())
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		raw, err := fs.ReadFile(c.embedded, e.Name())
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (c *Catalog) List() []Info {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return nil
	}
	out := make([]Info, 0, len(entries))
	for _, e := range entries {
		id := packID(e.Name())
		if id == "" || e.IsDir() {
			continue
		}
		pack, ok := c.load(id)
		if !ok {
			continue
		}
		name := pack.Name
		if name == "" {
			name = id
		}
		out = append(out, Info{ID: id, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Merged overlays the selected language on the bundled Simplified Chinese pack. Missing keys stay in Chinese.
func (c *Catalog) Merged(id string) map[string]string {
	base, _ := c.load("zh-CN")
	out := map[string]string{}
	for k, v := range base.Strings {
		out[k] = v
	}
	if id == "" || id == "zh-CN" {
		return out
	}
	pack, ok := c.load(id)
	if !ok {
		return out
	}
	for k, v := range pack.Strings {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

func (c *Catalog) Resolve(id string) string {
	if _, ok := c.load(id); ok {
		return id
	}
	if _, ok := c.load("zh-CN"); ok {
		return "zh-CN"
	}
	list := c.List()
	if len(list) > 0 {
		return list[0].ID
	}
	return "zh-CN"
}

func (c *Catalog) load(id string) (Pack, bool) {
	if !idPattern.MatchString(id) {
		return Pack{}, false
	}
	raw, err := os.ReadFile(filepath.Join(c.Dir, id+".json"))
	if err != nil {
		return Pack{}, false
	}
	var pack Pack
	if err := json.Unmarshal(raw, &pack); err != nil {
		return Pack{}, false
	}
	if pack.Strings == nil {
		pack.Strings = map[string]string{}
	}
	pack.ID = id
	return pack, true
}

func packID(name string) string {
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		return ""
	}
	id := strings.TrimSuffix(name, filepath.Ext(name))
	if !idPattern.MatchString(id) {
		return ""
	}
	return id
}

func hasPack(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if packID(e.Name()) != "" {
			return true
		}
	}
	return false
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".np-locale-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

func isGoRun(exe string) bool {
	return strings.Contains(exe, "go-build")
}
