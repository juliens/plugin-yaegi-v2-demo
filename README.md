# Yaegi v2 Plugin Demo

An example of Traefik middleware plugin run by [Yaegi v2](https://github.com/traefik/yaegi/tree/v2).
Use it as a template for your own plugin.

Yaegi v2 comes with Traefik v3.8. It is opt-in: Traefik uses it for a plugin that asks for it in its manifest.

The middleware removes the request headers whose name starts with one of the configured prefixes.

## Why Yaegi v2

The code uses an iterator (`maps.Keys`, Go 1.23) and a generic function of the standard library (`slices.ContainsFunc`):

```go
for name := range maps.Keys(req.Header) {
	if slices.ContainsFunc(h.prefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
		req.Header.Del(name)
	}
}
```

Yaegi v1 cannot load this plugin (`undefined selector: maps.Keys`).
Yaegi v2 uses the Go type checker, so it runs it as compiled Go does.

## Ask for Yaegi v2

Add `runtimeVersion: v2` to the manifest [`.traefik.yml`](.traefik.yml):

```yaml
displayName: Yaegi v2 Demo
type: middleware
runtimeVersion: v2
import: github.com/juliens/plugin-yaegi-v2-demo
basePkg: plugindemo
```

Without this line, Traefik runs the plugin with Yaegi v1.
`v2` is the only accepted value.

## Use this template

1. Create a repository from this template.
2. Change the module name in `go.mod`, and `import` in `.traefik.yml`, to `github.com/<owner>/<repo>`.
3. Change `basePkg` in `.traefik.yml` if you change the package name.
4. Change `testData` in `.traefik.yml`: it is the configuration used by the Plugin Catalog and the CI.

The CI fails while the module name does not match the GitHub repository: the Plugin Catalog has the same rule.

## Configuration

Static configuration:

```yaml
experimental:
  plugins:
    demo:
      moduleName: github.com/juliens/plugin-yaegi-v2-demo
      version: v0.1.0
```

Dynamic configuration:

```yaml
http:
  middlewares:
    demo:
      plugin:
        demo:
          prefixes:
            - X-Internal-
```

## CI

[`.github/workflows/main.yml`](.github/workflows/main.yml) has two jobs:

- `Lint and test`: `golangci-lint`, `go test` (compiled Go), and a check that `go.mod` is tidy.
- `Run with Yaegi v2`: the checks of the Traefik Plugin Catalog, then a load of the plugin with Yaegi v2, with the options of Traefik.

The second job runs [`.github/plugincheck`](.github/plugincheck).
It is a separate Go module, because the plugin itself must not depend on Yaegi or Traefik.
It checks:

- the manifest: type, `import`, `displayName`, `summary`, `testData`, and `runtimeVersion: v2`;
- `go.mod`: no dependency on Yaegi or Traefik, and `import` matches the module name;
- the module name matches the GitHub repository;
- the plugin loads with Yaegi v2 (`host.Options()`, as Traefik);
- `CreateConfig` works, `testData` decodes into the configuration, and `New` returns an `http.Handler` within 10 seconds;
- with `-request`, a GET request to this path returns the expected status (`-status`, default 200).

Run it locally:

```bash
cd .github/plugincheck && go run . -dir ../.. -request /
```

The Go version must match the AOT copies of the `yaegi/v2` version in `.github/plugincheck/go.mod` (today Go 1.27.3).
When you update `yaegi/v2`, update the Go version of the job too.
