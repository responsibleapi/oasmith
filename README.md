# OASmith

OASmith generates focused Go, TypeScript, Rust, and Dart code from OpenAPI YAML or JSON
documents.
It supports focused generation modes without the runtime and configuration
surface of a general-purpose OpenAPI generator.

## Supported output

| Mode | Language | Output |
| --- | --- | --- |
| `types` | `go` | Go models |
| `client` | `go` | Go models and HTTP client |
| `client` | `typescript` | TypeScript types and HTTP client |
| `types` | `rust` | Serde models in `mod.rs` |
| `client` | `rust` | Serde models and a Reqwest client in `mod.rs` |
| `types` | `dart` | Dart JSON models in `models.dart` |
| `client` | `dart` | Dart JSON models and an injectable HTTP client in `models.dart` and `api.dart` |

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
- `--lang`: `go`, `typescript`, `rust`, or `dart`, subject to the supported pairs above;
- `--out`: generated output directory.

JSON input is supported alongside YAML. The document syntax is accepted
directly, so `.json` and `.yaml` file names work with the same command.

TypeScript output is written directly from the embedded templates without
external tools.

Dart client output uses `package:http` and accepts an application-owned
`send(http.BaseRequest)` function. Pass the application's authenticated transport
to preserve its proxy, tracing, certificate, and cancellation behavior. The
optional `responseError` callback translates non-success statuses to the
application's error types. Operation methods encode paths and query parameters,
decode typed JSON models, and return SSE responses as streams for the caller to
frame. JSON response reads have a 4 MiB bound.
The Dart SDK is required when emitting Dart output so OASmith can format the
generated files.

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
`fetch` implementation to a TypeScript client. Rust clients accept a
middleware-enabled HTTP client at construction.
These examples assume the application has initialized an OpenTelemetry SDK;
`@opentelemetry/api` alone uses no-op tracing and propagation implementations.

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

### Rust

The insertion point is the **HTTP client passed to `api::Client::new`**. Configure
[`reqwest-tracing`](https://docs.rs/reqwest-tracing/0.5.8/reqwest_tracing/)
middleware once; every awaited generated operation propagates the active
trace automatically:

```rust
use api::Response as _;

let http = reqwest_middleware::ClientBuilder::new(reqwest::Client::builder().build()?)
    .with(reqwest_tracing::TracingMiddleware::default())
    .build();

let api = api::Client::new(http, "https://api.example.com".into(), None);

// Inside the application's existing tracing span:
match api.create_thing(params).await? {
    api::CreateThingResponse::Status201(thing) => println!("{}", thing.name),
    api::CreateThingResponse::Status400(problem) => println!("{}", problem.message),
    response => return Err(response.into_error().await.into()),
}
```

For the generated Reqwest 0.12 client, use `reqwest-middleware` 0.4.2 with its
`json` feature and `reqwest-tracing` 0.5.8 with its `opentelemetry_0_30` feature.
The application must already have OpenTelemetry 0.30, a `tracing-opentelemetry`
0.31 subscriber layer, and a registered W3C `TraceContextPropagator`.

The middleware reads the current `tracing` span when the request is sent, creates
a child HTTP span, and injects its `traceparent` and `tracestate` headers.
One shared client can therefore serve calls from different traces. Normal async
context propagation still applies: instrument spawned tasks with
[`.in_current_span()`](https://docs.rs/tracing/latest/tracing/trait.Instrument.html#method.in_current_span).

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

Moon sets `GOCACHE` to `~/.cache/go-build` on macOS and Linux, shared across
worktrees. Modules use Go's shared module cache (`go env GOMODCACHE`). Moon uses
`~/.cache/golangci-lint` for linter data; temporary files use the system temp
directory. The same cache locations apply in standalone and parent workspaces.
The lint task allows parallel runners without the global linter lock.
Moon task results
are not restored across CI runs. Sources, module files, fixtures, templates, and
configuration determine affected tasks; `$CI` is not an input. CI verifies
formatting, and selected Go tests bypass Go's test-result cache. Native Moon
reports are attached to the workflow, including on failure.

## License

[MIT](LICENSE)

## Rust clients

Use `--mode client --lang rust --out src/api`, then `mod api;`. Add `serde` 1
(with `derive`), `serde_json` 1, `reqwest` 0.12, and `reqwest-middleware` 0.4.2
(with `json`) to Cargo dependencies.
Choose the Reqwest TLS features appropriate to your application.

Construct `api::Client::new(http, base_url, bearer_token)` with your configured
`reqwest_middleware::ClientWithMiddleware`. For a client without middleware,
construct it with `reqwest_middleware::ClientBuilder::new(http).build()`.
Await an operation directly: `api.operation(params).await?` returns its
operation-specific response enum, with typed JSON, raw bytes or an
empty body for every declared status. Match the variants you want to handle;
`Response::into_error()` explicitly turns another variant into a diagnostic error.
Import the generated `Response` trait to use `status()` and `into_error()`.
Undeclared statuses retain the original Reqwest response in `Unexpected`.
SSE responses also remain unread Reqwest responses, leaving event framing and
cancellation to the caller.

JSON and raw bodies are buffered up to 4 MiB by default; `.body_limit(bytes)`
changes the cap. Invalid JSON and oversized bodies return decode/limit errors.
Configure application transport policy once with `Client::with_transport`.
Implement the generated `Transport` trait to apply cancellation or diagnostics
across sending and typed response decoding. Operations still take only their
parameters and return their response enum directly.
See [Rust trace propagation](#rust) to configure automatic propagation once.

Rust supports JSON and raw request bodies, optional bodies, scalar and repeated
query parameters, headers, escaped path parameters, enums, nullable values and
untagged `oneOf` models. Sequential multipart requests currently fail generation
with an explicit unsupported-body error. Run `moon run oasmith:test-rust` to generate,
compile, and execute the Rust contract fixtures.
