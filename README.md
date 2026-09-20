# OASmith

OASmith generates focused Go and TypeScript code from OpenAPI YAML or JSON
documents.
It supports focused generation modes without the runtime and configuration
surface of a general-purpose OpenAPI generator.

## Supported output

| Mode | Language | Output |
| --- | --- | --- |
| `types` | `go` | Go models |
| `client` | `go` | Go models and HTTP client |
| `client` | `typescript` | TypeScript types and HTTP client |

OASmith handles the OpenAPI schema and operation subset covered by its fixture
suite, including objects, arrays, enums, `oneOf` discriminators, parameters,
request bodies, responses, and server-sent event operations.

## Install

OASmith requires Go 1.26 or newer.

```sh
go install github.com/responsibleapi/oasmith/cmd/oasmith@latest
```

## Usage

```sh
oasmith \
  --openapi ./openapi.yaml \
  --mode client \
  --lang go \
  --out ./gen/client
```

Every invocation requires:

- `--openapi`: input OpenAPI YAML or JSON document;
- `--mode`: `types` or `client`;
- `--lang`: `go` or `typescript`, subject to the supported pairs above;
- `--out`: generated output directory.

JSON input is supported alongside YAML. The document syntax is accepted
directly, so `.json` and `.yaml` file names work with the same command.

TypeScript output is written directly from the embedded templates without
external tools.

Generated clients require an explicit client base URL and use it for every
operation. OpenAPI server declarations do not change the runtime destination.

TypeScript clients emit JSON bodies, raw bodies as `BodyInit`, and fixed-length
ordered multipart bodies declared with `prefixItems` and `prefixEncoding`.
Binary multipart parts are `Blob` values; their media types must match the
content types declared by the corresponding prefix encoding. Unsupported
request-body shapes fail generation.

## OpenTelemetry trace propagation

Generated clients leave OpenTelemetry dependencies and SDK setup to the
application. Pass an instrumented transport to a Go client or an instrumented
`fetch` implementation to a TypeScript client. These examples assume the
application has initialized an OpenTelemetry SDK; `@opentelemetry/api` alone
uses no-op tracing and propagation implementations.

### Go

Install `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`, wrap an
explicit HTTP transport, and pass the active `context.Context` to every
generated operation:

```go
transport := otelhttp.NewTransport(http.DefaultTransport)
httpClient := &http.Client{Transport: transport}

client, err := apiclient.NewClient(
	apiclient.ClientOptions{BaseURL: "https://api.example.com"},
	apiclient.WithHTTPClient(httpClient),
)
if err != nil {
	return err
}

ctx, span := otel.Tracer("example-app").Start(ctx, "create thing")
defer span.End()

_, err = client.CreateThing(ctx, params)
return err
```

The generated request keeps the operation context. `otelhttp.Transport` reads
its span context and injects the configured propagation headers, such as
`traceparent`, before sending the request. Pass the derived `ctx`; replacing it
with `context.Background()` breaks the parent trace.

### TypeScript

The generated `ClientOptions.fetch` hook can ask the registered global
propagator to inject the active OpenTelemetry context immediately before
transport execution:

```typescript
import { context, propagation, trace } from '@opentelemetry/api';
import { DefaultApi } from './gen/api.ts';

const baseFetch = globalThis.fetch;
const otelFetch: typeof globalThis.fetch = async (input, init) => {
    const request = new Request(input, init);
    const headers = new Headers(request.headers);
    propagation.inject(context.active(), headers, {
        set(carrier, key, value): void {
            carrier.set(key, value);
        },
    });
    return await baseFetch(new Request(request, { headers }));
};

const api = new DefaultApi({
    baseURL: 'https://api.example.com',
    fetch: otelFetch,
});

const tracer = trace.getTracer('example-app');
await tracer.startActiveSpan('create thing', async span => {
    try {
        await api.createThing(params);
    } finally {
        span.end();
    }
});
```

The application SDK must register both a context manager and a text-map
propagator. `context.active()` must contain a valid span context, and a W3C Trace
Context propagator must be registered for `propagation.inject` to add
`traceparent`; otherwise it adds no trace header. For browser calls across
origins, the API's CORS policy must also allow the propagation headers configured
by the application, commonly `traceparent`, `tracestate`, and `baggage`.

## Develop

[Go](https://go.dev) builds the generator and runs its tests.
[Moon](https://moonrepo.dev) runs the complete project check.

```sh
moon run check
```

CI follows [Moon's CI guide](https://moonrepo.dev/docs/guides/ci): full Git
history, source-based affected selection, and plain `moon ci`. It runs the same
project tasks used locally; aggregate checks and maintenance commands are excluded
from automatic selection. The same `moon.yml` also works as a Listenbox submodule.

Go modules, compiler output, and golangci-lint data are cached. Moon task results
are not restored across CI runs. Sources, module files, fixtures, templates, and
configuration determine affected tasks; `$CI` is not an input. CI verifies
formatting, and selected Go tests bypass Go's test-result cache. Native Moon
reports are attached to the workflow, including on failure.

## License

[MIT](LICENSE)
