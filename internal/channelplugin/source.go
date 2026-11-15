package channelplugin

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const SourceOperationID = ":source"

type SourceRequest struct {
	Source  string `json:"source"`
	Version string `json:"version"`
}

type Source struct {
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Version string `json:"version"`
}

var sourceReference = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+/-]*$`)
var repositoryName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`)

func ParseSource(value, version string) (Source, error) {
	value, version = strings.TrimSpace(value), strings.TrimSpace(version)
	if len(value) == 0 || len(value) > 8192 || strings.ContainsAny(value, "\x00\r\n\\") || len(version) > 128 {
		return Source{}, errors.New("请输入有效的 NPM 包名、GitHub 仓库或安装包地址")
	}
	setVersion := func(inline string) error {
		if inline != "" && version != "" && inline != version {
			return errors.New("来源中指定的版本与更多选项中的版本不一致")
		}
		if inline != "" {
			version = inline
		}
		if version != "" && (!sourceReference.MatchString(version) || strings.Contains(version, "..")) {
			return errors.New("版本必须是固定版本、标签、分支或 commit")
		}
		return nil
	}
	github := strings.HasPrefix(value, "github:")
	if github {
		value = "https://github.com/" + strings.TrimPrefix(value, "github:")
	}
	if strings.Contains(value, "://") {
		u, err := sourceURL(value)
		if err != nil {
			return Source{}, err
		}
		if strings.EqualFold(u.Hostname(), "github.com") {
			if strings.Contains(u.Path, "/releases/download/") || strings.Contains(u.Path, "/archive/") {
				if version != "" || u.Fragment != "" {
					return Source{}, errors.New("安装包地址不支持单独指定版本")
				}
				return Source{Kind: "archive", Address: u.String()}, nil
			}
			if u.Port() != "" || u.RawQuery != "" {
				return Source{}, errors.New("GitHub 来源必须是仓库地址，可用 # 指定版本")
			}
			repo := strings.TrimSuffix(strings.Trim(u.Path, "/"), ".git")
			if !repositoryName.MatchString(repo) {
				return Source{}, errors.New("GitHub 仓库格式应为 github:owner/repo 或 https://github.com/owner/repo")
			}
			for _, part := range strings.Split(repo, "/") {
				if part == "." || part == ".." {
					return Source{}, errors.New("GitHub 仓库名称无效")
				}
			}
			if err := setVersion(u.Fragment); err != nil {
				return Source{}, err
			}
			return Source{Kind: "github", Address: "https://github.com/" + repo, Version: version}, nil
		}
		if github || u.Fragment != "" || version != "" {
			return Source{}, errors.New("安装包地址不支持单独指定版本，请填写对应版本的下载地址")
		}
		return Source{Kind: "archive", Address: u.String()}, nil
	}
	if strings.HasPrefix(value, "git@") || strings.HasPrefix(value, "git+") {
		return Source{}, errors.New("当前只支持公开 GitHub 仓库的 HTTPS 地址，不支持安装凭据")
	}
	name := value
	if at := strings.LastIndex(name, "@"); at > 0 {
		if at == len(name)-1 {
			return Source{}, errors.New("NPM 版本或标签无效")
		}
		if err := setVersion(name[at+1:]); err != nil {
			return Source{}, err
		}
		name = name[:at]
	} else if err := setVersion(""); err != nil {
		return Source{}, err
	}
	if strings.Contains(version, "/") {
		return Source{}, errors.New("NPM 版本或标签无效")
	}
	d := Descriptor{ID: "source", Format: "aicat", Package: name, Version: "1.0.0", Registry: registry()}
	if err := validateDescriptor(d); err != nil {
		return Source{}, err
	}
	return Source{Kind: "npm", Address: name, Version: version}, nil
}

func sourceURL(value string) (*url.URL, error) {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("安装地址必须使用不含账号密码的 HTTP 或 HTTPS URL")
	}
	return u, nil
}

func DisplaySource(value string) string {
	u, err := url.Parse(value)
	if err == nil && u.Host != "" {
		u.RawQuery, u.Fragment, u.User = "", "", nil
		return u.String()
	}
	return value
}

func sourceBytes(ctx context.Context, address string, limit int64) ([]byte, error) {
	if _, err := sourceURL(address); err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 3 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		req.Header.Del("Referer")
		if len(via) >= 5 {
			return errors.New("下载重定向过多")
		}
		_, err := sourceURL(req.URL.String())
		return err
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", sourceEndpoint(address), nil)
	if err != nil {
		return nil, errors.New("安装地址无效")
	}
	req.Header.Set("Accept", "application/json, application/octet-stream")
	req.Header.Set("User-Agent", "aicat-extension-installer")
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("无法连接安装来源，请检查地址、网络或下载链接是否过期")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return nil, fmt.Errorf("此来源需要认证或访问被拒绝，当前不支持安装凭据（HTTP %d）", response.StatusCode)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载来源不存在或不可公开访问（HTTP %d）", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("下载中断，请重试")
	}
	if int64(len(data)) > limit {
		return nil, errors.New("安装来源响应超过大小限制")
	}
	return data, nil
}

type registryPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Dist    struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
		Shasum    string `json:"shasum"`
	} `json:"dist"`
}

func downloadSource(ctx context.Context, source Source) ([]byte, string, string, error) {
	address, revision := source.Address, ""
	var pkg registryPackage
	if source.Kind == "npm" {
		ref := source.Version
		if ref == "" {
			ref = "latest"
		}
		data, err := sourceBytes(ctx, strings.TrimRight(registry(), "/")+"/"+url.PathEscape(source.Address)+"/"+url.PathEscape(ref), 8<<20)
		if err != nil {
			return nil, "", "", err
		}
		if json.Unmarshal(data, &pkg) != nil {
			return nil, "", "", errors.New("NPM 来源返回的包信息无效")
		}
		if pkg.Version == "" {
			var metadata struct {
				Tags     map[string]string          `json:"dist-tags"`
				Versions map[string]registryPackage `json:"versions"`
			}
			if json.Unmarshal(data, &metadata) != nil {
				return nil, "", "", errors.New("NPM 来源返回的包信息无效")
			}
			if target := metadata.Tags[ref]; target != "" {
				ref = target
			}
			pkg = metadata.Versions[ref]
		}
		if pkg.Name != source.Address || !validVersion(pkg.Version) || pkg.Dist.Tarball == "" {
			return nil, "", "", errors.New("NPM 包或版本不存在")
		}
		address = pkg.Dist.Tarball
	}
	if source.Kind == "github" {
		repo := strings.TrimPrefix(source.Address, "https://github.com/")
		ref := source.Version
		if ref == "" {
			ref = "HEAD"
		}
		data, err := sourceBytes(ctx, "https://api.github.com/repos/"+repo+"/commits/"+url.PathEscape(ref), 1<<20)
		if err != nil {
			return nil, "", "", err
		}
		var commit struct {
			SHA string `json:"sha"`
		}
		if json.Unmarshal(data, &commit) != nil || len(commit.SHA) != 40 || strings.Trim(commit.SHA, "0123456789abcdef") != "" {
			return nil, "", "", errors.New("GitHub 返回的版本信息无效")
		}
		revision = commit.SHA
		address = "https://api.github.com/repos/" + repo + "/tarball/" + revision
	}
	data, err := sourceBytes(ctx, address, 64<<20)
	if err != nil {
		return nil, "", "", err
	}
	if source.Kind == "npm" {
		matched := false
		if pkg.Dist.Integrity != "" {
			for _, integrity := range strings.Fields(pkg.Dist.Integrity) {
				algorithm, expected, ok := strings.Cut(integrity, "-")
				if !ok {
					continue
				}
				var actual []byte
				switch algorithm {
				case "sha512":
					sum := sha512.Sum512(data)
					actual = sum[:]
				case "sha256":
					sum := sha256.Sum256(data)
					actual = sum[:]
				case "sha1":
					sum := sha1.Sum(data)
					actual = sum[:]
				}
				if actual != nil && base64.StdEncoding.EncodeToString(actual) == expected {
					matched = true
				}
			}
		} else if pkg.Dist.Shasum != "" {
			sum := sha1.Sum(data)
			matched = hex.EncodeToString(sum[:]) == pkg.Dist.Shasum
		}
		if !matched {
			return nil, "", "", errors.New("NPM 安装包完整性校验失败")
		}
	}
	return data, revision, pkg.Version, nil
}

func sourceDescriptor(data []byte, source Source, expectedVersion string) (Descriptor, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return Descriptor{}, errors.New("安装包不是有效的 .tgz / .tar.gz 归档")
	}
	defer gz.Close()
	reader := tar.NewReader(io.LimitReader(gz, 256<<20+1))
	var manifest []byte
	root := ""
	var total int64
	seen := map[string]bool{}
	for count := 0; ; count++ {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || count >= 10000 {
			return Descriptor{}, errors.New("安装包内容无效或超过大小限制")
		}
		name := strings.TrimPrefix(header.Name, "./")
		if name == "." || name == "" {
			continue
		}
		clean := path.Clean(name)
		if strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(name, "\\") || strings.ContainsRune(name, 0) || (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir) || (seen[clean] && header.Typeflag != tar.TypeDir) {
			return Descriptor{}, errors.New("安装包包含越界路径、链接或不支持的文件")
		}
		seen[clean] = true
		prefix, rest, hasRoot := strings.Cut(clean, "/")
		if root == "" {
			root = prefix
		}
		if root != prefix || (!hasRoot && header.Typeflag != tar.TypeDir) {
			return Descriptor{}, errors.New("安装包必须包含单一根目录")
		}
		total += header.Size
		if header.Size < 0 || total > 256<<20 {
			return Descriptor{}, errors.New("安装包解压后超过大小限制")
		}
		if rest == "package.json" && header.Typeflag == tar.TypeReg {
			if header.Size > 256<<10 {
				return Descriptor{}, errors.New("渠道包声明文件过大")
			}
			manifest, err = io.ReadAll(reader)
			if err != nil {
				return Descriptor{}, errors.New("渠道包声明文件不可读")
			}
		}
	}
	var pkg struct {
		Name        string    `json:"name"`
		Version     string    `json:"version"`
		Description string    `json:"description"`
		Aicat       *Manifest `json:"aicat"`
	}
	if json.Unmarshal(manifest, &pkg) != nil || pkg.Aicat == nil || (expectedVersion != "" && pkg.Version != expectedVersion) {
		return Descriptor{}, errors.New("安装包缺少匹配的 aicat 渠道声明")
	}
	d := Descriptor{Format: "aicat", ID: pkg.Aicat.ID, Name: pkg.Name, Summary: pkg.Description, Package: pkg.Name, Version: pkg.Version, Registry: registry(), Source: source.Address, Reference: source.Version}
	if len(d.Summary) > 1024 {
		return Descriptor{}, errors.New("渠道包简介过长")
	}
	if strings.HasPrefix(d.Package, "@") {
		d.Publisher = strings.Split(strings.TrimPrefix(d.Package, "@"), "/")[0]
	}
	return d, validateDescriptor(d)
}

func (s *Store) InstallSource(ctx context.Context, request SourceRequest, guard func(Descriptor) error) (Descriptor, error) {
	source, err := ParseSource(request.Source, request.Version)
	if err != nil {
		return Descriptor{}, err
	}
	var descriptor Descriptor
	err = s.operate(ctx, SourceOperationID, "install", func(ctx context.Context) error {
		var err error
		descriptor, err = s.prepareSource(ctx, source, SourceOperationID, guard)
		return err
	})
	return descriptor, err
}

func (s *Store) updateSource(ctx context.Context, id string, current Descriptor) error {
	source, err := ParseSource(current.Source, current.Reference)
	if err != nil {
		return err
	}
	_, err = s.prepareSource(ctx, source, id, func(next Descriptor) error {
		if next.ID != id || next.Package != current.Package || next.Format != current.Format {
			return errors.New("安装来源中的扩展身份已改变")
		}
		return nil
	})
	return err
}

func (s *Store) prepareSource(ctx context.Context, source Source, operation string, guard func(Descriptor) error) (Descriptor, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	s.stage(operation, "downloading")
	data, revision, expected, err := downloadSource(ctx, source)
	if err != nil {
		return Descriptor{}, err
	}
	s.stage(operation, "verifying")
	d, err := sourceDescriptor(data, source, expected)
	if err != nil {
		return Descriptor{}, err
	}
	if source.Kind == "npm" && d.Package != source.Address {
		return Descriptor{}, errors.New("NPM 安装包身份与来源不一致")
	}
	if err = guard(d); err != nil {
		return Descriptor{}, err
	}
	parent := filepath.Join(s.home, "extensions")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return Descriptor{}, err
	}
	workspace, err := os.MkdirTemp(parent, ".source-")
	if err != nil {
		return Descriptor{}, err
	}
	defer os.RemoveAll(workspace)
	archive := filepath.Join(workspace, "package.tgz")
	if err = os.WriteFile(archive, data, 0600); err != nil {
		return Descriptor{}, err
	}
	stage := filepath.Join(workspace, "install")
	if err = os.Mkdir(stage, 0700); err != nil {
		return Descriptor{}, err
	}
	installed, err := s.installInto(ctx, d, stage, "file:"+archive, operation)
	if err != nil {
		return Descriptor{}, err
	}
	sum := sha256.Sum256(data)
	installed.SourceSHA256, installed.Revision = hex.EncodeToString(sum[:]), revision
	installed.Directory = installed.Version + "-" + installed.SourceSHA256[:16] + "-" + installed.LockSHA256[:8]
	if err = s.activate(ctx, operation); err != nil {
		return Descriptor{}, err
	}
	if err = s.register(d, true); err != nil {
		return Descriptor{}, err
	}
	if err = s.commitInstallation(ctx, d.ID, operation, stage, installed); err != nil {
		return Descriptor{}, err
	}
	return d, nil
}
