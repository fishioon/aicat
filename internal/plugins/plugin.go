package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
)

const MaxSourceBytes = 512 << 10

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
var compileSlots = make(chan struct{}, 2)

func ValidID(id string) bool { return identifier.MatchString(id) }

type Plugin struct {
	Info  Info
	mod   *moejs.Module
	hooks map[string]moejs.Hook
}

func Compile(ctx context.Context, manifest Manifest, source string) (*Plugin, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if !ValidID(manifest.ID) || manifest.APIVersion != 1 || manifest.Name == "" || manifest.Version == "" || len(manifest.Name) > 128 || len(manifest.Version) > 128 {
		return nil, errors.New("plugin requires apiVersion 1 and valid id, name and version")
	}
	if manifest.Main != "index.js" && manifest.Main != "index.ts" {
		return nil, errors.New("plugin main must be index.js or index.ts")
	}
	if len(source) > MaxSourceBytes {
		return nil, errors.New("plugin source exceeds 512 KiB")
	}
	select {
	case compileSlots <- struct{}{}:
		defer func() { <-compileSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	mod, err := moejs.CompileWith(manifest.Main, source, moejs.CompileOptions{Context: ctx, TypeScript: strings.HasSuffix(manifest.Main, ".ts")})
	if err != nil {
		return nil, err
	}
	if len(mod.Requests()) != 0 {
		return nil, errors.New("v1 plugins must be a single module; imports are not supported")
	}
	hash := sha256.Sum256([]byte(source))
	p := &Plugin{Info: Info{Manifest: manifest, Hash: hex.EncodeToString(hash[:]), Hooks: []string{}}, mod: mod, hooks: make(map[string]moejs.Hook)}
	for _, stage := range Stages {
		hook, err := mod.Hook(stage)
		if errors.Is(err, moejs.ErrHookNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		p.hooks[stage] = hook
		p.Info.Hooks = append(p.Info.Hooks, stage)
	}
	if len(p.hooks) == 0 {
		return nil, errors.New("plugin exports none of the four supported hooks")
	}
	return p, nil
}
func Load(ctx context.Context, home, id string) (*Plugin, string, error) {
	m, source, err := Read(home, id)
	if err != nil {
		return nil, "", err
	}
	p, err := Compile(ctx, m, source)
	return p, source, err
}

// Read retains editable source even if it cannot currently compile.
func Read(home, id string) (Manifest, string, error) {
	if !ValidID(id) {
		return Manifest{}, "", errors.New("invalid plugin id")
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return Manifest{}, "", err
	}
	defer root.Close()
	directory, err := root.OpenRoot(filepath.Join("plugins", id))
	if err != nil {
		return Manifest{}, "", err
	}
	defer directory.Close()
	b, err := read(directory, "plugin.json", 64<<10)
	if err != nil {
		return Manifest{}, "", err
	}
	var m Manifest
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&m); err != nil {
		return Manifest{}, "", err
	}
	if err = decoder.Decode(new(any)); err != io.EOF {
		return Manifest{}, "", errors.New("plugin manifest must contain one JSON object")
	}
	if m.ID != id {
		return Manifest{}, "", errors.New("plugin directory and manifest id differ")
	}
	if m.Main != "index.js" && m.Main != "index.ts" {
		return Manifest{}, "", errors.New("invalid plugin entry")
	}
	b, err = read(directory, m.Main, MaxSourceBytes)
	if err != nil {
		return Manifest{}, "", err
	}
	return m, string(b), nil
}
func read(root *os.Root, path string, max int64) ([]byte, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("plugin files must be regular files")
	}
	b, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("plugin file exceeds limit")
	}
	return b, nil
}
func (p *Plugin) Has(stage string) bool { _, ok := p.hooks[stage]; return ok }

// Each invocation owns a fresh realm. Compiled bytecode is shared, mutable
// globals never cross requests/hooks, and context is explicitly returned.
func (p *Plugin) Call(ctx context.Context, stage string, input Message, settings map[string]any, limits Limits) (output Message, logs []CallLog, err error) {
	hook, ok := p.hooks[stage]
	if !ok {
		return input, nil, nil
	}
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: limits.MemoryBytes, DisableDynamicCode: true})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return output, nil, err
	}
	defer loop.Terminate()
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { rt.Interrupt(ctx.Err()); loop.StopNoWait(); close(finished) })
	defer func() {
		if !stop() {
			<-finished
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}()
	logFn := func(level string) moejs.NativeFunc {
		return func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			if len(logs) >= 16 {
				return moejs.Undefined(), nil
			}
			parts := []string{}
			size := 0
			for _, arg := range args {
				s, e := r.ToString(arg)
				if e != nil {
					return moejs.Undefined(), e
				}
				v := s.GoString()
				if len(v) > 1024-size {
					v = v[:1024-size]
				}
				parts = append(parts, v)
				size += len(v)
				if size >= 1024 {
					break
				}
			}
			logs = append(logs, CallLog{level, strings.Join(parts, " ")})
			return moejs.Undefined(), nil
		}
	}
	text := moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		v := moejs.Arg(args, 0)
		if !v.IsString() {
			return moejs.Undefined(), errors.New("bodyText expects a base64 string")
		}
		encoded := v.String()
		if int64(len(encoded)) > limits.ResultBytes {
			return moejs.Undefined(), errors.New("body helper input exceeds limit")
		}
		b, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil {
			return moejs.Undefined(), e
		}
		if !utf8.Valid(b) {
			return moejs.Undefined(), errors.New("body is not UTF-8")
		}
		return moejs.String(string(b)), nil
	})
	fromText := moejs.NativeFunc(func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		v := moejs.Arg(args, 0)
		if !v.IsString() {
			return moejs.Undefined(), errors.New("bodyFromText expects a string")
		}
		value := v.String()
		if int64(base64.StdEncoding.EncodedLen(len(value))) > limits.ResultBytes {
			return moejs.Undefined(), errors.New("body helper output exceeds limit")
		}
		return moejs.String(base64.StdEncoding.EncodeToString([]byte(value))), nil
	})
	if err = rt.SetGlobal("console", map[string]any{"log": logFn("info"), "info": logFn("info"), "warn": logFn("warn"), "error": logFn("error")}); err != nil {
		return output, logs, err
	}
	if settings == nil {
		settings = map[string]any{}
	}
	if err = rt.SetGlobal("host", map[string]any{"settings": settings, "bodyText": text, "bodyFromText": fromText}); err != nil {
		return output, logs, err
	}
	err = loop.Run(func(rt *moejs.Runtime) error {
		e := rt.Load(p.mod)
		if errors.Is(e, moejs.ErrModulePending) {
			return nil
		}
		return e
	})
	if err != nil {
		return output, logs, err
	}
	var result moejs.Value
	err = loop.Run(func(rt *moejs.Runtime) error {
		arg, e := rt.FromGo(input)
		if e != nil {
			return e
		}
		result, e = rt.Call(hook, arg)
		return e
	})
	if err != nil {
		return output, logs, err
	}
	if state, value, promise := moejs.PromiseResult(result); promise {
		switch state {
		case moejs.PromiseFulfilled:
			result = value
		case moejs.PromiseRejected:
			return output, logs, fmt.Errorf("plugin promise rejected: %s", value.String())
		default:
			return output, logs, errors.New("plugin returned an unresolved promise")
		}
	}
	if result.IsUndefined() {
		return output, logs, errors.New("hook must return its message")
	}
	err = rt.UnmarshalWith(result, &output, moejs.DecodeOptions{MaxBytes: int(limits.ResultBytes), MaxNodes: 200000})
	return output, logs, err
}
