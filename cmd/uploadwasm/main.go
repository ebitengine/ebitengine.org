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

package main

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"golang.org/x/sync/errgroup"
)

var (
	flagEbitenginePath = flag.String("ebitenginepath", "", "path to ebiten repository")
	flagUpload         = flag.Bool("upload", false, "upload binary files to the server")
)

func examples() ([]string, error) {
	f, err := os.Open(filepath.Join("contents", "en", "examples"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	names, err := f.Readdirnames(0)
	if err != nil {
		return nil, err
	}

	var es []string
	for _, n := range names {
		if !strings.HasSuffix(n, ".json") {
			continue
		}
		ext := filepath.Ext(n)
		es = append(es, n[:len(n)-len(ext)])
	}

	return es, nil
}

const (
	bucket    = "res-ebitengine-org"
	keyPrefix = "wasm/"
)

var wasmExecQueryRE = regexp.MustCompile(`wasm_exec\.js\?[^"]*`)

// updateWasmExec copies wasm_exec.js from the toolchain that builds the example
// binaries and rewrites the cache-busting query string in _wasm.html to the new
// file's content hash. dir selects the module whose GOROOT (and thus toolchain
// version) is used, matching how the examples are built.
func updateWasmExec(dir string) error {
	cmd := exec.Command("go", "env", "GOROOT")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	goroot := strings.TrimSpace(string(out))

	// Go 1.24 moved wasm_exec.js from misc/wasm to lib/wasm.
	src := filepath.Join(goroot, "lib", "wasm", "wasm_exec.js")
	if _, err := os.Stat(src); err != nil {
		src = filepath.Join(goroot, "misc", "wasm", "wasm_exec.js")
	}

	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join("dist", "scripts", "wasm_exec.js"), content, 0o644); err != nil {
		return err
	}

	h := fnv.New128a()
	h.Write(content)
	hash := base64.RawURLEncoding.EncodeToString(h.Sum(nil))[:10]

	htmlPath := filepath.Join("dist", "_wasm.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		return err
	}
	html = wasmExecQueryRE.ReplaceAll(html, []byte("wasm_exec.js?v="+hash))
	if err := os.WriteFile(htmlPath, html, 0o644); err != nil {
		return err
	}

	fmt.Printf("Updated wasm_exec.js from %s (hash %s)\n", src, hash)
	return nil
}

var wasmURLVersionRE = regexp.MustCompile(`\$\{name\}\.wasm(\?v=[0-9A-Za-z_-]*)?`)

// stampWasmVersion rewrites the cache-busting query string on the .wasm URL in
// _wasm.html to a hash over all built binaries. Without it a stale browser cache
// could serve a binary that mismatches the freshly cache-busted wasm_exec.js
// loader. names holds the example base names; the binaries are dir/<name>.wasm.
func stampWasmVersion(dir string, names []string) error {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	h := fnv.New128a()
	for _, name := range sorted {
		content, err := os.ReadFile(filepath.Join(dir, name+".wasm"))
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00", name)
		h.Write(content)
	}
	hash := base64.RawURLEncoding.EncodeToString(h.Sum(nil))[:10]

	htmlPath := filepath.Join("dist", "_wasm.html")
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		return err
	}
	html = wasmURLVersionRE.ReplaceAllLiteral(html, []byte("${name}.wasm?v="+hash))
	if err := os.WriteFile(htmlPath, html, 0o644); err != nil {
		return err
	}

	fmt.Printf("Stamped .wasm version %s\n", hash)
	return nil
}

func newS3Client(ctx context.Context) (*s3.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("auto"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(os.Getenv("R2_ACCESS_KEY_ID"), os.Getenv("R2_SECRET_ACCESS_KEY"), "")),
		// R2 does not support the checksums the SDK sends by default.
		config.WithRequestChecksumCalculation(aws.RequestChecksumCalculationWhenRequired),
		config.WithResponseChecksumValidation(aws.ResponseChecksumValidationWhenRequired),
	)
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(fmt.Sprintf("https://%s.r2.cloudflarestorage.com", os.Getenv("R2_ACCOUNT_ID")))
	}), nil
}

func uploadFile(ctx context.Context, client *s3.Client, name string, r io.Reader) error {
	fmt.Printf("Uploading %s...\n", name)

	if _, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:          aws.String(bucket),
		Key:             aws.String(keyPrefix + name),
		Body:            r,
		ContentType:     aws.String("application/wasm"),
		ContentEncoding: aws.String("gzip"),
	}); err != nil {
		return err
	}

	return nil
}

func run() error {
	if *flagUpload {
		for _, env := range []string{"R2_ACCOUNT_ID", "R2_ACCESS_KEY_ID", "R2_SECRET_ACCESS_KEY"} {
			if os.Getenv(env) == "" {
				return fmt.Errorf("%s must be set", env)
			}
		}

		if err := updateWasmExec(*flagEbitenginePath); err != nil {
			return err
		}
	}

	es, err := examples()
	if err != nil {
		return err
	}

	tmpout, err := os.MkdirTemp("", "")
	if err != nil {
		return err
	}
	fmt.Printf("Temporary directory: %s\n", tmpout)
	if *flagUpload {
		defer os.RemoveAll(tmpout)
	}

	ctx := context.Background()

	var s3Client *s3.Client
	if *flagUpload {
		var err error
		s3Client, err = newS3Client(ctx)
		if err != nil {
			return err
		}
	}

	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}

	ch := make(chan string, n)

	var g errgroup.Group
	g.Go(func() error {
		defer close(ch)

		var g errgroup.Group
		for _, e := range es {
			e := e
			g.Go(func() error {
				name := e + ".wasm"
				args := []string{
					"build",
					"-o", filepath.Join(tmpout, name) + ".tmp",
					"./examples/" + e,
				}
				fmt.Println("go", strings.Join(args, " "))
				cmd := exec.Command("go", args...)
				cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
				cmd.Dir = *flagEbitenginePath
				cmd.Stderr = os.Stderr

				if err := cmd.Run(); err != nil {
					return err
				}

				in, err := os.Open(filepath.Join(tmpout, name+".tmp"))
				if err != nil {
					return err
				}
				defer in.Close()

				out, err := os.Create(filepath.Join(tmpout, name))
				if err != nil {
					return err
				}

				w := gzip.NewWriter(out)
				if _, err := io.Copy(w, in); err != nil {
					out.Close()
					return err
				}

				// Flush and close before sending the name to the channel so that
				// the consumer reads a fully written file. Otherwise the file may
				// still grow while it is being uploaded, breaking the upload's
				// Content-Length.
				if err := w.Close(); err != nil {
					out.Close()
					return err
				}
				if err := out.Close(); err != nil {
					return err
				}

				if err := os.Remove(filepath.Join(tmpout, name+".tmp")); err != nil {
					return err
				}

				ch <- name

				return nil
			})
		}
		return g.Wait()
	})
	g.Go(func() error {
		var once sync.Once
		semaphore := make(chan struct{}, n)

		var g errgroup.Group
		for name := range ch {
			name := name
			if !*flagUpload {
				once.Do(func() {
					fmt.Printf("Binary files are not uploaded. To upload this, specify -upload.\n")
				})
				continue
			}
			semaphore <- struct{}{}
			g.Go(func() error {
				defer func() {
					<-semaphore
				}()

				f, err := os.Open(filepath.Join(tmpout, name))
				if err != nil {
					return err
				}
				defer f.Close()

				if err := uploadFile(ctx, s3Client, name, f); err != nil {
					return err
				}

				return nil
			})
		}
		return g.Wait()
	})

	if err := g.Wait(); err != nil {
		return err
	}

	if *flagUpload {
		if err := stampWasmVersion(tmpout, es); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	flag.Parse()
	if *flagEbitenginePath == "" {
		fmt.Fprintln(os.Stderr, "Specify -ebitenginepath")
		os.Exit(1)
	}

	if err := run(); err != nil {
		panic(err)
	}
}
