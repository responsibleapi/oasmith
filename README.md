# OAScribe

OAScribe generates focused Go and TypeScript code from OpenAPI YAML documents.
It supports the generation modes used by Listenbox without the runtime and
configuration surface of a general-purpose OpenAPI generator.

## Supported output

| Mode | Language | Output |
| --- | --- | --- |
| `types` | `go` | Go models |
| `client` | `go` | Go models and HTTP client |
| `client` | `typescript` | TypeScript types and HTTP client |

OAScribe handles the OpenAPI schema and operation subset covered by its fixture
suite, including objects, arrays, enums, `oneOf` discriminators, parameters,
request bodies, responses, and server-sent event operations.

## Install

OAScribe requires Go 1.26 or newer.

```sh
go install github.com/meoyawn/oascribe/cmd/oascribe@latest
```

## Usage

```sh
oascribe \
  --openapi ./openapi.yaml \
  --mode client \
  --lang go \
  --out ./gen/client
```

Every invocation requires:

- `--openapi`: input OpenAPI YAML document;
- `--mode`: `types` or `client`;
- `--lang`: `go` or `typescript`, subject to the supported pairs above;
- `--out`: generated output directory.

When `nubx` is available, OAScribe runs its pinned Oxfmt version through
`nubx`'s local discovery and registry fallback. No Node project or installed
Oxfmt dependency is required. Generation still works without `nubx`.

## Develop

[Nub](https://nubjs.com) resolves pinned Oxfmt and Vitest versions on demand.
[Task](https://taskfile.dev) runs the complete project check.

```sh
task check
```

## License

[MIT](LICENSE)
