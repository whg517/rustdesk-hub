package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

type viteChunk struct {
	File           string   `json:"file"`
	CSS            []string `json:"css"`
	Assets         []string `json:"assets"`
	Imports        []string `json:"imports"`
	DynamicImports []string `json:"dynamicImports"`
}

func ValidateWebBuild(build fs.FS) error {
	index, err := readBuildFile(build, "index.html")
	if err != nil {
		return err
	}
	contents, err := readBuildFile(build, ".vite/manifest.json")
	if err != nil {
		return err
	}
	var manifest map[string]viteChunk
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return fmt.Errorf("Vite 构建清单无效 %w", err)
	}
	entry := manifest["index.html"]
	if entry.File == "" {
		return errors.New("Vite 构建清单缺少首页入口")
	}
	for _, chunk := range manifest {
		for _, dependency := range slices.Concat(chunk.Imports, chunk.DynamicImports) {
			if _, ok := manifest[dependency]; !ok {
				return fmt.Errorf("Vite 构建清单缺少依赖 %q", dependency)
			}
		}
		for _, name := range slices.Concat([]string{chunk.File}, chunk.CSS, chunk.Assets) {
			if err := checkAssetFile(build, name); err != nil {
				return err
			}
		}
	}
	return checkIndexAssets(build, index, manifest)
}

func indexDependencies(manifest map[string]viteChunk) (css, preloads map[string]bool) {
	css, preloads = make(map[string]bool), make(map[string]bool)
	visited := make(map[string]bool)
	pending := []string{"index.html"}
	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[name] {
			continue
		}
		visited[name] = true
		chunk := manifest[name]
		for _, style := range chunk.CSS {
			css[style] = true
		}
		for _, dep := range chunk.Imports {
			preloads[manifest[dep].File] = true
		}
		pending = append(pending, chunk.Imports...)
	}
	return css, preloads
}

func checkIndexAssets(build fs.FS, index []byte, manifest map[string]viteChunk) error {
	tokenizer := html.NewTokenizer(bytes.NewReader(index))
	entry := manifest["index.html"].File
	css, preloads := indexDependencies(manifest)
	foundEntry := false
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if err := tokenizer.Err(); !errors.Is(err, io.EOF) {
				return err
			}
			if !foundEntry {
				return errors.New("首页引用的入口脚本与 Vite 构建清单不匹配")
			}
			for name, missing := range css {
				if missing {
					return fmt.Errorf("首页缺少 Vite 构建样式 %q", name)
				}
			}
			return nil
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			var relation string
			for _, attr := range token.Attr {
				if attr.Key == "rel" {
					relation = strings.ToLower(attr.Val)
				}
			}
			for _, attr := range token.Attr {
				if attr.Key != "src" && attr.Key != "href" {
					continue
				}
				name, isAsset := strings.CutPrefix(attr.Val, "/")
				if !isAsset || !strings.HasPrefix(name, "assets/") {
					continue
				}
				if token.Data == "script" && attr.Key == "src" {
					if name != entry {
						return fmt.Errorf("首页引用的脚本与 Vite 构建清单不匹配 %q", name)
					}
					foundEntry = true
				}
				if token.Data == "link" && attr.Key == "href" {
					switch relation {
					case "stylesheet":
						if _, ok := css[name]; !ok {
							return fmt.Errorf("首页引用的样式与 Vite 构建清单不匹配 %q", name)
						}
						css[name] = false
					case "modulepreload":
						if !preloads[name] {
							return fmt.Errorf("首页预加载的脚本与 Vite 构建清单不匹配 %q", name)
						}
					}
				}
				if err := checkAssetFile(build, name); err != nil {
					return err
				}
			}
		}
	}
}

func readBuildFile(build fs.FS, name string) ([]byte, error) {
	data, err := fs.ReadFile(build, name)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("构建文件无效 %q", name)
	}
	return data, nil
}

func checkAssetFile(build fs.FS, name string) error {
	if !strings.HasPrefix(name, "assets/") || !fs.ValidPath(name) {
		return fmt.Errorf("Vite 构建文件路径无效 %q", name)
	}
	info, err := fs.Stat(build, name)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("构建文件无效 %q", name)
	}
	return nil
}
