package skin

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,40}$`)

// Meta comes from skin.json in each skin folder.
type Meta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Author      string `json:"author"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Accent      string `json:"accent"`
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
			if dir := filepath.Join(cwd, "skins"); hasSkin(dir) {
				return dir
			}
		}
	}
	if dir := filepath.Join(exeDir, "skins"); hasSkin(dir) {
		return dir
	}
	if dir := filepath.Join(cwd, "skins"); hasSkin(dir) {
		return dir
	}
	if writable(exeDir) {
		return filepath.Join(exeDir, "skins")
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(cwd, "skins")
	}
	return filepath.Join(cfg, "NetPulsar", "skins")
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

// Ensure copies bundled skin files that are not on disk yet. Existing files are left alone.
func (c *Catalog) Ensure() error {
	if c.embedded == nil {
		return nil
	}
	return fs.WalkDir(c.embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == "." {
			return err
		}
		dest := filepath.Join(c.Dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if _, err := os.Stat(dest); err == nil {
			return nil
		}
		raw, err := fs.ReadFile(c.embedded, path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, raw, 0o644)
	})
}

// Restore replaces one bundled skin with the copy embedded in the executable.
func (c *Catalog) Restore(id string) error {
	if !ValidID(id) {
		return errors.New("invalid skin id")
	}
	if c.embedded == nil {
		return errors.New("no bundled skins")
	}
	root := id
	if _, err := fs.Stat(c.embedded, root); err != nil {
		return errors.New("skin is not bundled")
	}
	return fs.WalkDir(c.embedded, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(c.Dir, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		raw, err := fs.ReadFile(c.embedded, path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, raw, 0o644)
	})
}

func (c *Catalog) List() []Meta {
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return nil
	}
	out := make([]Meta, 0, len(entries))
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		meta, err := c.readMeta(ent.Name())
		if err != nil {
			continue
		}
		out = append(out, meta)
	}
	return out
}

func (c *Catalog) Get(id string) (Meta, bool) {
	if !ValidID(id) {
		return Meta{}, false
	}
	meta, err := c.readMeta(id)
	if err != nil {
		return Meta{}, false
	}
	return meta, true
}

func (c *Catalog) Resolve(preferred string) Meta {
	if meta, ok := c.Get(preferred); ok {
		return meta
	}
	if meta, ok := c.Get("aurora"); ok {
		return meta
	}
	list := c.List()
	if len(list) > 0 {
		return list[0]
	}
	return Meta{ID: "aurora", Name: "Aurora", Width: 340, Height: 230, Accent: "#8eb6ff"}
}

func (c *Catalog) Read(id, name string) ([]byte, error) {
	if !ValidID(id) || !safeFile(name) {
		return nil, os.ErrNotExist
	}
	return os.ReadFile(filepath.Join(c.Dir, id, name))
}

func (c *Catalog) readMeta(id string) (Meta, error) {
	raw, err := os.ReadFile(filepath.Join(c.Dir, id, "skin.json"))
	if err != nil {
		return Meta{}, err
	}
	var meta Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Meta{}, err
	}
	if meta.ID == "" {
		meta.ID = id
	}
	if meta.Name == "" {
		meta.Name = meta.ID
	}
	if meta.Width < 200 {
		meta.Width = 320
	}
	if meta.Height < 60 {
		meta.Height = 180
	}
	return meta, nil
}

func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

func safeFile(name string) bool {
	if name == "" || strings.Contains(name, "..") || strings.ContainsAny(name, `/\`) {
		return false
	}
	return true
}

func hasSkin(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, ent := range entries {
		if ent.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, ent.Name(), "skin.json")); err == nil {
				return true
			}
		}
	}
	return false
}

func isGoRun(exe string) bool {
	return strings.Contains(strings.ToLower(exe), "go-build")
}

func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".np-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}
