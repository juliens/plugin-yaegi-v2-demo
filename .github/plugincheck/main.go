// Command plugincheck runs the checks of the Traefik Plugin Catalog (Piceus) on a plugin,
// and loads the plugin with Yaegi v2 as Traefik does.
//
// Usage, from the root of the plugin:
//
//	cd .github/plugincheck && go run . -dir ../.. -repo owner/name -request /path
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/traefik/yaegi/v2/host"
	"github.com/traefik/yaegi/v2/vm"
	"golang.org/x/mod/modfile"
	"gopkg.in/yaml.v3"
)

const (
	typeMiddleware = "middleware"
	typeProvider   = "provider"

	// Same timeout as Piceus for the call of New.
	newTimeout = 10 * time.Second
)

// Manifest is the content of .traefik.yml.
type Manifest struct {
	DisplayName   string         `yaml:"displayName"`
	Type          string         `yaml:"type"`
	Runtime       string         `yaml:"runtime"`
	Import        string         `yaml:"import"`
	BasePkg       string         `yaml:"basePkg"`
	Compatibility string         `yaml:"compatibility"`
	Summary       string         `yaml:"summary"`
	UseUnsafe     bool           `yaml:"useUnsafe"`
	TestData      map[string]any `yaml:"testData"`
	YaegiVersion  string         `yaml:"yaegiVersion"`
}

func main() {
	dir := flag.String("dir", ".", "root directory of the plugin")
	repo := flag.String("repo", "", "GitHub repository (owner/name); when set, the module name must match it")
	request := flag.String("request", "", "path of a GET request sent to the middleware after its creation")
	status := flag.Int("status", http.StatusOK, "expected status code of the request")
	flag.Parse()

	log.SetFlags(0)

	if err := run(*dir, *repo, *request, *status); err != nil {
		log.Fatalf("FAIL: %v", err)
	}

	log.Println("OK: the plugin is valid and runs with Yaegi v2")
}

func run(dir, repo, request string, status int) error {
	manifest, err := readManifest(filepath.Join(dir, ".traefik.yml"))
	if err != nil {
		return err
	}

	mod, err := readModule(filepath.Join(dir, "go.mod"))
	if err != nil {
		return err
	}

	if err := checkModuleFile(mod, manifest); err != nil {
		return err
	}

	if repo != "" {
		if err := checkRepoName(repo, mod.Module.Mod.Path, manifest); err != nil {
			return err
		}
	}

	log.Println("manifest and go.mod: ok")

	// Like Traefik and Piceus, put the sources in a GOPATH.
	goPath, err := os.MkdirTemp("", "plugincheck-gopath")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(goPath) }()

	moduleDir := filepath.Join(goPath, "src", filepath.FromSlash(mod.Module.Mod.Path))
	if err := copyDir(dir, moduleDir); err != nil {
		return fmt.Errorf("copying the sources: %w", err)
	}

	plugin, err := load(goPath, moduleDir, manifest)
	if err != nil {
		return fmt.Errorf("failed to load the plugin with Yaegi v2: %w", err)
	}
	defer func() { _ = plugin.Close() }()

	log.Println("load with Yaegi v2: ok")

	switch manifest.Type {
	case typeMiddleware:
		return checkMiddleware(plugin, manifest, request, status)
	case typeProvider:
		return checkProvider(plugin, manifest)
	default:
		return fmt.Errorf("unsupported type: %s", manifest.Type)
	}
}

// readManifest reads the manifest and does the checks of Piceus.
func readManifest(file string) (*Manifest, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("missing manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(content, &m); err != nil {
		return nil, fmt.Errorf("failed to read manifest content: %w", err)
	}

	switch {
	case m.Type != typeMiddleware && m.Type != typeProvider:
		return nil, fmt.Errorf("unsupported type: %s", m.Type)
	case m.Runtime != "" && m.Runtime != "yaegi":
		return nil, fmt.Errorf("unsupported runtime for a Yaegi plugin: %s", m.Runtime)
	case m.Import == "":
		return nil, errors.New("missing import")
	case m.DisplayName == "":
		return nil, errors.New("missing DisplayName")
	case m.Summary == "":
		return nil, errors.New("missing Summary")
	case m.TestData == nil:
		return nil, errors.New("missing TestData")
	case m.YaegiVersion != "v2":
		return nil, fmt.Errorf("yaegiVersion must be v2 in the manifest, got %q", m.YaegiVersion)
	}

	return &m, nil
}

func readModule(file string) (*modfile.File, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("missing go.mod: %w", err)
	}

	return modfile.Parse("go.mod", content, nil)
}

// checkModuleFile is the go.mod check of Piceus.
func checkModuleFile(mod *modfile.File, manifest *Manifest) error {
	forbidden := []string{
		"github.com/containous/yaegi",
		"github.com/containous/traefik",
		"github.com/containous/maesh",
		"github.com/traefik/yaegi",
		"github.com/traefik/traefik",
		"github.com/traefik/mesh",
	}

	for _, require := range mod.Require {
		for _, f := range forbidden {
			if strings.Contains(require.Mod.Path, f) {
				return fmt.Errorf("a plugin cannot have a dependence to: %s", require.Mod.Path)
			}
		}
	}

	if !strings.HasPrefix(strings.ReplaceAll(manifest.Import, "-", "_"), strings.ReplaceAll(mod.Module.Mod.Path, "-", "_")) {
		return fmt.Errorf("the import %q must be related to the module name %q", manifest.Import, mod.Module.Mod.Path)
	}

	return nil
}

// checkRepoName is the repository check of Piceus.
func checkRepoName(repo, moduleName string, manifest *Manifest) error {
	repoName := path.Join("github.com", repo)

	if !strings.HasPrefix(moduleName, repoName) {
		return fmt.Errorf("the module name (%s) doesn't contain the GitHub repository name (%s)", moduleName, repoName)
	}

	if !strings.HasPrefix(manifest.Import, repoName) {
		return fmt.Errorf("the import name (%s) doesn't contain the GitHub repository name (%s)", manifest.Import, repoName)
	}

	return nil
}

// load loads the plugin with the options of Traefik.
func load(goPath, moduleDir string, manifest *Manifest) (*vm.Plugin, error) {
	opts := host.Options()
	opts.Root = filepath.Join(goPath, "src")
	opts.Dir = moduleDir
	opts.UseUnsafe = manifest.UseUnsafe
	opts.Stdout = prefixWriter("plugin stdout: ")
	opts.Stderr = prefixWriter("plugin stderr: ")
	opts.GoPanic = func(r any) {
		log.Printf("panic in a goroutine of the plugin: %v", r)
	}

	plugin, err := host.LoadContext(context.Background(), opts, manifest.Import)
	if err != nil {
		return nil, err
	}

	for _, note := range plugin.Notes {
		log.Printf("note: %s", note)
	}

	return plugin, nil
}

func checkMiddleware(plugin *vm.Plugin, manifest *Manifest, request string, status int) error {
	config, err := createConfig(plugin, manifest.TestData)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), newTimeout)
	defer cancel()

	next := http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	})

	result, err := callNew(ctx, plugin, ctx, next, config, "test")
	if err != nil {
		return err
	}

	v, err := result.Interface()
	if err != nil {
		return err
	}

	handler, ok := v.(http.Handler)
	if !ok {
		return fmt.Errorf("invalid handler type: %T", v)
	}

	log.Println("CreateConfig and New: ok")

	if request == "" {
		return nil
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, request, nil))

	log.Printf("GET %s: %d\n%s", request, rec.Code, rec.Body.String())

	if rec.Code != status {
		return fmt.Errorf("GET %s: got status %d, want %d", request, rec.Code, status)
	}

	return nil
}

func checkProvider(plugin *vm.Plugin, manifest *Manifest) error {
	config, err := createConfig(plugin, manifest.TestData)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), newTimeout)
	defer cancel()

	if _, err := callNew(ctx, plugin, ctx, config, "test"); err != nil {
		return err
	}

	log.Println("CreateConfig and New: ok")

	return nil
}

// createConfig calls CreateConfig and decodes the test data in the result, as Traefik does.
func createConfig(plugin *vm.Plugin, testData map[string]any) (*vm.Object, error) {
	results, err := plugin.Call(context.Background(), "CreateConfig")
	if err != nil {
		return nil, fmt.Errorf("failed to call CreateConfig: %w", err)
	}

	if len(results) != 1 {
		return nil, fmt.Errorf("invalid number of return for the CreateConfig function: %d", len(results))
	}

	config := results[0]

	err = config.Decode(func(ptr any) error {
		decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			DecodeHook:       mapstructure.StringToSliceHookFunc(","),
			WeaklyTypedInput: true,
			Result:           ptr,
		})
		if err != nil {
			return err
		}

		return decoder.Decode(testData)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to decode the test data: %w", err)
	}

	return config, nil
}

// callNew calls the New function of the plugin and returns its first result.
func callNew(ctx context.Context, plugin *vm.Plugin, args ...any) (*vm.Object, error) {
	results, err := plugin.Call(ctx, "New", args...)
	if err != nil {
		return nil, fmt.Errorf("failed to call New: %w", err)
	}

	if len(results) != 2 {
		return nil, fmt.Errorf("invalid number of return for the New function: got %d, want 2", len(results))
	}

	v, err := results[1].Interface()
	if err != nil {
		return nil, err
	}

	if v != nil {
		resultErr, ok := v.(error)
		if !ok {
			return nil, fmt.Errorf("invalid error type: %T", v)
		}

		return nil, fmt.Errorf("failed to create a new plugin instance: %w", resultErr)
	}

	return results[0], nil
}

// copyDir copies the plugin sources, without the .git directory.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}

		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}

		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		if !d.Type().IsRegular() {
			return nil
		}

		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}

	return out.Close()
}

type prefixWriter string

func (p prefixWriter) Write(b []byte) (int, error) {
	log.Print(string(p) + strings.TrimRight(string(b), "\n"))
	return len(b), nil
}
