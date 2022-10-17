// Copyright 2019 Hajime Hoshi
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gen

import (
	"bytes"
	"encoding/json"
	"errors"
	"html/template"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const outDir = "_site"

func cleanup() error {
	return filepath.Walk(outDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".html" {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), "_") {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		return nil
	})
}

func Run(url, description string) error {
	tmpl, err := template.New("tmpl.html").Funcs(template.FuncMap{
		"noescape": func(str string) template.HTML {
			return template.HTML(str)
		},
	}).ParseFiles("tmpl.html")
	if err != nil {
		return err
	}

	if err := cleanup(); err != nil {
		return err
	}

	var langDirs []string
	ents, err := os.ReadDir("contents")
	if err != nil {
		return err
	}
	for _, ent := range ents {
		if ent.IsDir() {
			langDirs = append(langDirs, ent.Name())
		}
	}

	if err := filepath.Walk("contents", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		content := ""

		switch filepath.Ext(path) {
		case ".html":
			if filepath.Base(path) == "tmpl.html" {
				return nil
			}
			if filepath.Base(path) == "nav.html" {
				return nil
			}

			c, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}
			content = string(c)

		case ".json":
			t, err := template.ParseFiles(filepath.Join(filepath.Dir(path), "tmpl.html"))
			if err != nil {
				return err
			}
			var j map[string]interface{}

			c, err := ioutil.ReadFile(path)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(c, &j); err != nil {
				return err
			}
			j["Base"] = filepath.Base(path[:len(path)-len(filepath.Ext(path))])

			b := &bytes.Buffer{}
			if err := t.Execute(b, j); err != nil {
				return err
			}
			content = string(b.Bytes())

		default:
			return nil
		}

		p, err := newPage([]byte(content), path)
		if err != nil {
			return err
		}

		origExt := filepath.Ext(path)
		rel, err := filepath.Rel("contents", path[:len(path)-len(origExt)]+".html")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(outDir, filepath.Dir(rel)), 0755); err != nil {
			return err
		}
		// TODO: What if the file already exists?
		w, err := os.Create(filepath.Join(outDir, rel))
		if err != nil {
			return err
		}
		defer w.Close()

		title, err := p.title()
		if err != nil {
			return err
		}

		canonical := ""
		switch rel {
		case "404.html":
			// No canonical URL
		case "index.html":
			canonical = url
		default:
			// When generated on a Windows machine, rel will have \ characters.
			// Use ToSlash to ensure that all path separators are /.
			canonical = url + "/" + filepath.ToSlash(rel)
			if strings.HasSuffix(canonical, "/index.html") {
				canonical = strings.TrimSuffix(canonical, "index.html")
			}
		}

		f := filepath.Join(filepath.Dir(path), "nav.html")
		c, err := ioutil.ReadFile(f)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		subnav := string(c)

		share := url + "/images/share.png"
		s, err := p.share()
		if err != nil {
			return err
		}
		if s != "" {
			share = url + s
		}

		lang := "en"
		if relDir := filepath.Dir(rel); len(relDir) > 0 && relDir != "." {
			if parts := strings.Split(relDir, string(filepath.Separator)); len(parts) > 0 {
				lang = parts[0]
			}
		}

		var altPages []AltPage
		for _, l := range langDirs {
			if l == lang {
				altPages = append(altPages, AltPage{
					Available: false,
					Lang:      lang,
					Title:     langName(lang),
				})
				continue
			}

			var altRel string
			if rel == "index.html" {
				// The top page for non-English
				altRel = filepath.Join(l, rel)
			} else {
				dir, file := filepath.Split(rel)
				if dir == lang+string(filepath.Separator) && file == "index.html" && l == "en" {
					// The top page for English
					altRel = "index.html"
				} else {
					altRel = l + rel[len(lang):]
				}
			}

			if _, err := os.Stat(filepath.Join("contents", altRel[:len(altRel)-len(".html")]+origExt)); err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					return err
				}
				continue
			}

			relURL := "/" + filepath.ToSlash(altRel)
			if strings.HasSuffix(relURL, "/index.html") {
				relURL = relURL[:len(relURL)-len("index.html")]
			}

			altPages = append(altPages, AltPage{
				Available: true,
				Lang:      l,
				Title:     langName(l),
				RelURL:    relURL,
				AbsURL:    url + relURL,
			})
		}

		sort.Slice(altPages, func(i, j int) bool {
			if altPages[i].Lang == "en" {
				return true
			}
			if altPages[j].Lang == "en" {
				return false
			}
			return altPages[i].Lang < altPages[j].Lang
		})

		top := "/"
		if lang != "en" {
			top += lang + "/"
		}

		if err := tmpl.Execute(w, map[string]interface{}{
			"Title":     title,
			"Desc":      description,
			"Content":   content,
			"Share":     share,
			"Canonical": canonical,
			"NavExists": p.hasNav(),
			"SubNav":    subnav,
			"Feedback":  p.hasFeedback(),
			"Redirect":  p.redirect(),
			"Lang":      lang,
			"Top":       top,
			"AltPages":  altPages,
		}); err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

type AltPage struct {
	Available bool
	Lang      string
	Title     string
	RelURL    string
	AbsURL    string
}

func langName(lang string) string {
	switch lang {
	case "en":
		return "English"
	case "ja":
		return "日本語"
	}
	return ""
}
