# OASmith

OASmith generates focused Go, TypeScript, and Rust code from OpenAPI YAML or JSON
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
- `--lang`: `go`, `typescript`, or `rust`, subject to the supported pairs above;
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
`fetch` implementation to a TypeScript client. Rust clients expose Reqwest request
builders for context injection or execution through application-owned middleware.
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

Generated operations return a normal
[`reqwest::RequestBuilder`](https://docs.rs/reqwest/0.12.28/reqwest/struct.RequestBuilder.html).
They prepare the URL, authentication, parameters, and body; the caller chooses
when and how to send the request. Plain Reqwest does not inject OpenTelemetry
headers automatically. Both approaches below work with the generated client
without changing generated code.

Rust has no standard equivalent of Go's `context.Context` that combines tracing,
deadlines, and cancellation, nor a standard global `fetch` hook. The
[`opentelemetry::Context`](https://docs.rs/opentelemetry/0.30.0/opentelemetry/context/struct.Context.html)
type carries telemetry context. Pass it explicitly, or use async instrumentation
to make a span current while a future is polled. Timeouts and cancellation remain
separate: use Reqwest's `.timeout(...)` and your async runtime's cancellation
mechanism. An OpenTelemetry context does not cancel HTTP requests.
The standard library's `std::task::Context` is an executor polling interface,
unrelated to request or trace context.

#### Propagate an explicit context

In addition to the [Rust client dependencies](#rust-clients), this example uses:

```toml
opentelemetry = "0.30"
opentelemetry_sdk = "0.30"
opentelemetry-http = "0.30"
```

Register the application's propagator once at startup:

```rust
opentelemetry::global::set_text_map_propagator(
    opentelemetry_sdk::propagation::TraceContextPropagator::new(),
);
```

This configures W3C `traceparent` and `tracestate` propagation. To propagate
baggage too, register a `TextMapCompositePropagator` containing both
`TraceContextPropagator` and `BaggagePropagator`. Propagator setup is separate from
initializing the application's tracing SDK and exporter.

Keep a send helper in the application. Reqwest's `build_split()` returns the
original HTTP client along with the built request, preserving its connection
pool and configuration:

```rust
use opentelemetry::{global, Context};
use opentelemetry_http::HeaderInjector;

async fn send_with_context(
    builder: reqwest::RequestBuilder,
    cx: &Context,
) -> Result<reqwest::Response, reqwest::Error> {
    let (http, request) = builder.build_split();
    let mut request = request?;
    global::get_text_map_propagator(|propagator| {
        propagator.inject_context(cx, &mut HeaderInjector(request.headers_mut()));
    });
    http.execute(request).await
}

async fn create_thing(
    api: &api::Client,
    params: api::CreateThingParams,
    cx: &Context,
) -> Result<api::CreateThingResponse, reqwest::Error> {
    let response = send_with_context(api.create_thing(params), cx).await?;
    api::CreateThingResponse::decode(response).await
}
```

Here `api` is the generated module; substitute your own operation and parameter
names. Pass the context of the span that should parent the remote operation.
This helper propagates that span; it does not create an HTTP client span. Without
a valid span context or a configured propagator, it adds no trace header. Inject
per request; putting a trace's headers in `ClientBuilder::default_headers` would
reuse that trace across unrelated calls.

For applications using `tracing`, obtain the context with
`tracing::Span::current().context()` after importing
`tracing_opentelemetry::OpenTelemetrySpanExt`. This requires a
`tracing-opentelemetry` subscriber layer connected to the SDK. A plain `tracing`
span, or `Context::current()` without an attached OpenTelemetry context, is not
enough. The compatible versions for the example above are `tracing` 0.1 and
`tracing-opentelemetry` 0.31.

#### Create HTTP spans and propagate through middleware

[`reqwest-middleware`](https://docs.rs/reqwest-middleware/0.4.2/reqwest_middleware/)
and [`reqwest-tracing`](https://docs.rs/reqwest-tracing/0.5.8/reqwest_tracing/)
provide automatic HTTP client spans and header injection. With the Reqwest 0.12
client, add these dependencies alongside OpenTelemetry 0.30 above:

```toml
reqwest-middleware = "0.4.2"
reqwest-tracing = { version = "0.5.8", features = ["opentelemetry_0_30"] }
tracing = "0.1"
tracing-opentelemetry = "0.31"
tracing-subscriber = "0.3"
```

These versions form a compatible set. The OpenTelemetry feature is required for
header injection; `TracingMiddleware` without it only creates `tracing` spans.
Keep Reqwest, middleware, and tracing integration versions aligned when upgrading.
Use versioned Reqwest documentation: this client's documented and tested
dependency is 0.12, while the `/latest/` documentation can describe a different
release.

Alongside the propagator setup above, connect the application's existing
`SdkTracerProvider` to `tracing` once at startup (or add the layer to your existing
subscriber). Keep the provider alive and shut it down at application exit to
flush exported spans:

```rust
use opentelemetry::trace::TracerProvider;
use tracing_subscriber::prelude::*;

tracing_subscriber::registry()
    .with(tracing_opentelemetry::layer().with_tracer(provider.tracer("example-app")))
    .try_init()?;
```

Share the configured Reqwest client between the generated request builders and
the middleware executor. Building a request does not send it:

```rust
use reqwest_middleware::ClientBuilder;
use reqwest_tracing::TracingMiddleware;
use tracing::Instrument;

let http = reqwest::Client::builder().build()?;
let api = api::Client::new(http.clone(), "https://api.example.com".into(), None);
let traced_http = ClientBuilder::new(http)
    .with(TracingMiddleware::default())
    .build();

let result: Result<api::CreateThingResponse, reqwest_middleware::Error> = async {
    let request = api.create_thing(params).build()?;
    let response = traced_http.execute(request).await?;
    api::CreateThingResponse::decode(response).await.map_err(Into::into)
}
.instrument(tracing::info_span!("create thing"))
.await;
```

Execute through `traced_http`; calling the generated builder's `.send()` sends
directly through plain Reqwest and bypasses middleware. The middleware creates a
child HTTP span under `create thing` and injects that child's context. Declared
error statuses still reach the generated response decoder. SSE responses still
stream; the HTTP middleware span ends when response headers arrive, so instrument
stream consumption separately if its lifetime matters.

Across `.await`, use
[`Instrument::instrument` / `.in_current_span()`](https://docs.rs/tracing/latest/tracing/trait.Instrument.html)
for `tracing`, or
[`FutureExt::with_context`](https://docs.rs/opentelemetry/0.30.0/opentelemetry/trace/trait.FutureExt.html)
for direct OpenTelemetry contexts. Do not hold a `Span::enter()` or
`Context::attach()` guard across `.await`: those guards set thread-local state,
which can associate another task's work with the wrong span. When spawning a
task, instrument its future explicitly; Tokio does not automatically inherit
the caller's current span.

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
(with `derive`), `serde_json` 1, and `reqwest` 0.12 (with `json`) to Cargo dependencies.
Choose the Reqwest TLS features appropriate to your application.

Construct `api::Client::new(http, base_url, bearer_token)` with your configured
Reqwest client. Operation methods return request builders, so callers own timeouts,
cancellation, tracing, and bounded body reads. Models and operation-specific
`Response::decode` enums preserve declared HTTP statuses; undeclared statuses
retain their original response. SSE responses remain streaming Reqwest responses,
leaving event framing and cancellation to the caller.
See [Rust trace propagation](#rust) for explicit context injection and automatic
HTTP tracing with middleware.

Rust supports JSON and raw request bodies, optional bodies, scalar and repeated
query parameters, headers, escaped path parameters, enums, nullable values and
untagged `oneOf` models. Sequential multipart requests currently fail generation
with an explicit unsupported-body error. Run `moon run oasmith:test-rust` to generate,
compile, and execute the Rust contract fixtures.
