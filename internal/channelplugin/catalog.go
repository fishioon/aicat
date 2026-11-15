package channelplugin

import (
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var catalogMu sync.Mutex

func (s *Store) Descriptors() ([]Descriptor, error) {
	result := append([]Descriptor{}, Catalog...)
	raw, err := os.ReadFile(filepath.Join(s.home, "channel-catalog.json"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	var added []Descriptor
	if json.Unmarshal(raw, &added) != nil {
		return nil, errors.New("已登记渠道包目录损坏")
	}
	seen := map[string]bool{}
	for _, d := range added {
		if err = validateDescriptor(d); err != nil {
			return nil, err
		}
		if seen[d.ID] {
			return nil, errors.New("渠道包标识重复")
		}
		seen[d.ID] = true
		merged := false
		for i, builtin := range Catalog {
			if builtin.ID != d.ID {
				continue
			}
			if d.Source == "" || d.Package != builtin.Package || d.Format != builtin.Format {
				return nil, errors.New("不能改变推荐渠道的包身份或格式")
			}
			result[i], merged = d, true
		}
		if !merged {
			result = append(result, d)
		}
	}
	return result, nil
}

func (s *Store) find(id string) (Descriptor, bool) {
	descriptors, err := s.Descriptors()
	if err != nil {
		return Descriptor{}, false
	}
	for _, d := range descriptors {
		if d.ID == id {
			return d, true
		}
	}
	return Descriptor{}, false
}

func (s *Store) Descriptor(id string) (Descriptor, bool) { return s.find(id) }

func validateDescriptor(d Descriptor) error {
	if len(d.ID) < 1 || len(d.ID) > 128 || d.ID == "." || d.ID == ".." || strings.Trim(d.ID, "abcdefghijklmnopqrstuvwxyz0123456789._-") != "" || !validVersion(d.Version) || (d.Format != "aicat" && d.Format != "openclaw") {
		return errors.New("渠道包标识、版本或格式无效")
	}
	name := strings.TrimPrefix(d.Package, "@")
	parts := strings.Split(name, "/")
	if d.Package == "" || len(d.Package) > 214 || len(parts) > 2 || (len(parts) == 2 && !strings.HasPrefix(d.Package, "@")) || (len(parts) == 1 && strings.HasPrefix(d.Package, "@")) {
		return errors.New("NPM 包名无效")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.Trim(part, "abcdefghijklmnopqrstuvwxyz0123456789._-") != "" {
			return errors.New("NPM 包名无效")
		}
	}
	if d.Source != "" {
		if _, err := ParseSource(d.Source, d.Reference); err != nil {
			return err
		}
	}
	u, err := url.Parse(d.Registry)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return errors.New("NPM registry 必须使用 HTTPS，本机测试服务除外")
	}
	return nil
}

func (s *Store) Register(d Descriptor) error {
	return s.register(d, false)
}

func (s *Store) register(d Descriptor, source bool) error {
	if err := validateDescriptor(d); err != nil {
		return err
	}
	catalogMu.Lock()
	defer catalogMu.Unlock()
	_, err := s.Descriptors()
	if err != nil {
		return err
	}
	for _, builtin := range Catalog {
		if builtin.ID == d.ID {
			if !source || builtin.Package != d.Package || builtin.Format != d.Format {
				return errors.New("不能覆盖推荐渠道的包身份或格式")
			}
			d.Name, d.Summary, d.Publisher = builtin.Name, builtin.Summary, builtin.Publisher
		}
	}
	added := []Descriptor{}
	raw, err := os.ReadFile(filepath.Join(s.home, "channel-catalog.json"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(raw) > 0 && json.Unmarshal(raw, &added) != nil {
		return errors.New("已登记渠道包目录损坏")
	}
	found := false
	for i, current := range added {
		if current.ID == d.ID {
			if current.Package != d.Package || current.Format != d.Format || (!source && (current.Registry != d.Registry || current.Source != d.Source)) {
				return errors.New("不能改变已登记渠道的包身份、格式或来源")
			}
			added[i], found = d, true
		}
	}
	if !found {
		added = append(added, d)
	}
	if err = os.MkdirAll(s.home, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.home, ".channel-catalog-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(added)
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), filepath.Join(s.home, "channel-catalog.json")); err != nil {
		return err
	}
	dir, err := os.Open(s.home)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
