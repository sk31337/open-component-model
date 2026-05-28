// Command shard enumerates e2e scenarios under examples/ and
// test/e2e/scenarios/, splits them into N round-robin shards, and emits a
// matrix=<json> line suitable for $GITHUB_OUTPUT.
//
// At Stage 1 of the migration plan, both roots are still empty of e2e.yaml
// files (legacy flat layout has not been moved yet), so this program emits
// an empty matrix. That is the expected output and is structural — it lets
// the GitHub Actions workflow be wired up before any scenario migrates.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const e2eYamlFile = "e2e.yaml"

func main() {
	shards := flag.Int("shards", 4, "number of shards to split scenarios into")
	repoRoot := flag.String("repo-root", "", "path to the controller module root (defaults to cwd)")
	flag.Parse()

	if *shards < 1 {
		fmt.Fprintln(os.Stderr, "shards must be >= 1")
		os.Exit(2)
	}

	root := *repoRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, "getwd:", err)
			os.Exit(1)
		}
	}

	roots := []string{
		filepath.Join(root, "examples"),
		filepath.Join(root, "test", "e2e", "scenarios"),
	}

	var scenarios []string
	for _, r := range roots {
		found, err := walk(r)
		if err != nil {
			fmt.Fprintf(os.Stderr, "walk %s: %v\n", r, err)
			os.Exit(1)
		}
		scenarios = append(scenarios, found...)
	}
	sort.Strings(scenarios)

	type shard struct {
		Shard int    `json:"shard"`
		Focus string `json:"focus"`
	}
	matrix := make([]shard, *shards)
	for i := range matrix {
		matrix[i] = shard{Shard: i, Focus: ""}
	}

	// Round-robin scenarios into shards. Empty shards get a sentinel focus
	// regex that matches nothing so the runner exits quickly.
	buckets := make([][]string, *shards)
	for i, s := range scenarios {
		buckets[i%*shards] = append(buckets[i%*shards], s)
	}
	for i, names := range buckets {
		if len(names) == 0 {
			matrix[i].Focus = "$.^" // matches nothing
			continue
		}
		matrix[i].Focus = "^(" + joinAlternation(names) + ")$"
	}

	out, err := json.Marshal(matrix)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal:", err)
		os.Exit(1)
	}
	fmt.Printf("matrix=%s\n", out)
}

func walk(root string) ([]string, error) {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, nil
	}
	var found []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, e2eYamlFile)); statErr == nil {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			found = append(found, filepath.ToSlash(rel))
			return fs.SkipDir
		}
		return nil
	})
	return found, err
}

func joinAlternation(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += "|"
		}
		// Names contain only path characters and slashes; no regex
		// metacharacters need escaping. If a future folder uses dots or
		// other meta, harden this with regexp.QuoteMeta.
		out += n
	}
	return out
}
