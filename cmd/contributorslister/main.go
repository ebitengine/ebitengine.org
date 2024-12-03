// Copyright 2024 Hajime Hoshi
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
	"cmp"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

func main() {
	if err := xmain(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

type commit struct {
	Author author `json:"author"`
}

type author struct {
	Login string `json:"login"`
}

func xmain() error {
	flag.Parse()
	repo := "hajimehoshi/ebiten"
	if flag.NArg() > 0 {
		repo = flag.Arg(0)
	}

	year := time.Now().Year()

	cmd := exec.Command("gh", "api",
		"-H", "Accept: application/vnd.github+json",
		"-H", "X-GitHub-Api-Version: 2022-11-28",
		"--paginate", "--slurp",
		fmt.Sprintf("/repos/%s/commits?since=%d-12-01T00:00:00Z&until=%d-12-01T00:00:00Z", repo, year-1, year))
	out, err := cmd.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("error: %s\n%s", e, e.Stderr)
		}
		return err
	}

	var commitArrays [][]commit
	if err := json.Unmarshal(out, &commitArrays); err != nil {
		return err
	}

	committerCounts := map[string]int{}
	for _, commits := range commitArrays {
		for _, c := range commits {
			committerCounts[c.Author.Login]++
		}
	}

	type committer struct {
		login string
		count int
	}
	var committers []committer
	for login, count := range committerCounts {
		committers = append(committers, committer{login, count})
	}

	slices.SortFunc(committers, func(a, b committer) int {
		if a.count != b.count {
			return -(a.count - b.count)
		}
		return cmp.Compare(strings.ToLower(a.login), strings.ToLower(b.login))
	})

	for _, c := range committers {
		fmt.Printf("%s: %d\n", c.login, c.count)
	}

	return nil
}
