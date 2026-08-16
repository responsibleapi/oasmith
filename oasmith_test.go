package oasmith_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/responsibleapi/oasmith/internal/goemit"
	"github.com/responsibleapi/oasmith/internal/openapi"
	"github.com/responsibleapi/oasmith/internal/tsemit"
)

func TestGoldenFixtures(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name    string
		fixture string
		golden  string
		emit    func(*openapi.Document, string, string) error
	}{
		{
			name:    "config-go",
			fixture: "config.yaml",
			golden:  "config-go",
			emit: func(doc *openapi.Document, outDir string, sourcePath string) error {
				return goemit.Emit(doc, goemit.Options{OutDir: outDir, SourcePath: sourcePath})
			},
		},
		{
			name:    "worker-go",
			fixture: "worker.yaml",
			golden:  "worker-go",
			emit: func(doc *openapi.Document, outDir string, sourcePath string) error {
				return goemit.Emit(doc, goemit.Options{OutDir: outDir, SourcePath: sourcePath})
			},
		},
		{
			name:    "worker-json-go",
			fixture: "worker.json",
			golden:  "worker-go",
			emit: func(doc *openapi.Document, outDir string, sourcePath string) error {
				return goemit.Emit(doc, goemit.Options{OutDir: outDir, SourcePath: sourcePath})
			},
		},
		{
			name:    "public-client-go",
			fixture: "public-client.yaml",
			golden:  "public-client-go",
			emit: func(doc *openapi.Document, outDir string, sourcePath string) error {
				return goemit.EmitClient(doc, goemit.Options{OutDir: outDir, SourcePath: sourcePath})
			},
		},
		{
			name:    "private-typescript",
			fixture: "private.yaml",
			golden:  "private-typescript",
			emit: func(doc *openapi.Document, outDir string, _ string) error {
				return tsemit.Emit(doc, tsemit.Options{OutDir: outDir})
			},
		},
		{
			name:    "config-typescript",
			fixture: "config.yaml",
			golden:  "config-typescript",
			emit: func(doc *openapi.Document, outDir string, _ string) error {
				return tsemit.Emit(doc, tsemit.Options{OutDir: outDir})
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			fixturePath := filepath.Join("testdata", "fixtures", testCase.fixture)
			doc, err := openapi.ParseFile(fixturePath)
			if err != nil {
				t.Fatalf("parse %s: %v", fixturePath, err)
			}
			outDir := filepath.Join(t.TempDir(), "out")
			if err := testCase.emit(doc, outDir, fixturePath); err != nil {
				t.Fatalf("emit %s: %v", testCase.name, err)
			}
			compareDirs(t, filepath.Join("testdata", "golden", testCase.golden), outDir)
		})
	}
}

func TestGoOneOfOutputForConfigDiscriminators(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("testdata", "fixtures", "config.yaml")
	doc, err := openapi.ParseFile(fixturePath)
	if err != nil {
		t.Fatalf("parse config fixture: %v", err)
	}
	outDir := t.TempDir()
	if err := goemit.Emit(doc, goemit.Options{OutDir: outDir, SourcePath: fixturePath}); err != nil {
		t.Fatalf("emit config go: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "models.go"))
	if err != nil {
		t.Fatalf("read models.go: %v", err)
	}
	source := string(raw)
	for _, want := range []string{
		"type ListenerConfig struct",
		"TcpListenerConfig     *TcpListenerConfig",
		"case \"tcp\":",
		"type MailerConfig struct",
		"StdoutMailerConfig *StdoutMailerConfig",
		"case \"ses\":",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("models.go missing %q", want)
		}
	}
}

func TestJSONFixtureGeneratesTypeScript(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("testdata", "fixtures", "worker.json")
	doc, err := openapi.ParseFile(fixturePath)
	if err != nil {
		t.Fatalf("parse JSON fixture: %v", err)
	}
	outDir := t.TempDir()
	if err := tsemit.Emit(doc, tsemit.Options{OutDir: outDir}); err != nil {
		t.Fatalf("emit TypeScript from JSON fixture: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "types.ts"))
	if err != nil {
		t.Fatalf("read generated TypeScript types: %v", err)
	}
	if !strings.Contains(string(raw), "export interface WorkerConfig") {
		t.Fatalf("generated TypeScript types missing WorkerConfig:\n%s", raw)
	}
}

func TestTypeScriptAPIInterceptors(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("testdata", "fixtures", "private.yaml")
	doc, err := openapi.ParseFile(fixturePath)
	if err != nil {
		t.Fatalf("parse private fixture: %v", err)
	}
	outDir := t.TempDir()
	if err := tsemit.Emit(doc, tsemit.Options{OutDir: outDir}); err != nil {
		t.Fatalf("emit private typescript: %v", err)
	}
	testPath := filepath.Join(outDir, "api.test.ts")
	if err := os.WriteFile(testPath, []byte(apiBehaviorTest), 0o644); err != nil {
		t.Fatalf("write api test: %v", err)
	}
	cmd := exec.Command("nubx", "-y", "vitest@4.1.10", "run", "--globals", "--root", outDir, "api.test.ts")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("typescript api test failed: %v\n%s", err, string(output))
	}
}

func TestTypeScriptClientQueries(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("testdata", "fixtures", "public-client.yaml")
	doc, err := openapi.ParseFile(fixturePath)
	if err != nil {
		t.Fatalf("parse public client fixture: %v", err)
	}
	outDir := t.TempDir()
	if err := tsemit.Emit(doc, tsemit.Options{OutDir: outDir}); err != nil {
		t.Fatalf("emit public typescript: %v", err)
	}
	apiPath := filepath.Join(outDir, "api.ts")
	raw, err := os.ReadFile(apiPath)
	if err != nil {
		t.Fatalf("read generated TypeScript API: %v", err)
	}
	if !strings.Contains(string(raw), "const queryParameters = new URLSearchParams()") {
		t.Fatal("generated TypeScript API does not use URLSearchParams")
	}
	testPath := filepath.Join(outDir, "query.test.ts")
	if err := os.WriteFile(testPath, []byte(typescriptQueryBehaviorTest), 0o644); err != nil {
		t.Fatalf("write TypeScript query test: %v", err)
	}
	cmd := exec.Command("nubx", "-y", "vitest@4.1.10", "run", "--globals", "--root", outDir, "query.test.ts")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("typescript query test failed: %v\n%s", err, string(output))
	}
}

func TestGoClientBehavior(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("testdata", "fixtures", "public-client.yaml")
	doc, err := openapi.ParseFile(fixturePath)
	if err != nil {
		t.Fatalf("parse public client fixture: %v", err)
	}
	outDir := t.TempDir()
	if err := goemit.EmitClient(doc, goemit.Options{OutDir: outDir, SourcePath: fixturePath}); err != nil {
		t.Fatalf("emit Go client: %v", err)
	}
	for name, source := range map[string]string{
		"go.mod":         "module generatedclient\n\ngo 1.26\n",
		"client_test.go": goClientBehaviorTest,
	} {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = outDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated Go client test failed: %v\n%s", err, string(output))
	}
}

func TestGoPackageNameFallsBackToSourceBasename(t *testing.T) {
	t.Parallel()

	doc := &openapi.Document{Info: openapi.Info{Title: "YouTube Data API v3"}}
	outDir := t.TempDir()
	if err := goemit.Emit(doc, goemit.Options{OutDir: outDir, SourcePath: "youtube.yaml"}); err != nil {
		t.Fatalf("emit YouTube models: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "models.go"))
	if err != nil {
		t.Fatalf("read models.go: %v", err)
	}
	if !strings.HasPrefix(string(raw), "package youtube\n") {
		t.Fatalf("models package declaration = %q, want youtube", strings.SplitN(string(raw), "\n", 2)[0])
	}
}

func compareDirs(t *testing.T, goldenDir string, outDir string) {
	t.Helper()

	var goldenFiles []string
	err := filepath.WalkDir(goldenDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(goldenDir, path)
		if err != nil {
			return err
		}
		goldenFiles = append(goldenFiles, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk golden dir %s: %v", goldenDir, err)
	}
	for _, rel := range goldenFiles {
		goldenRaw, err := os.ReadFile(filepath.Join(goldenDir, rel))
		if err != nil {
			t.Fatalf("read golden %s: %v", rel, err)
		}
		outRaw, err := os.ReadFile(filepath.Join(outDir, rel))
		if err != nil {
			t.Fatalf("read generated %s: %v", rel, err)
		}
		if string(outRaw) != string(goldenRaw) {
			t.Fatalf("%s mismatch\n--- golden\n%s\n--- generated\n%s", rel, string(goldenRaw), string(outRaw))
		}
	}
	var generatedFiles []string
	err = filepath.WalkDir(outDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(outDir, path)
		if err != nil {
			return err
		}
		generatedFiles = append(generatedFiles, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walk generated dir %s: %v", outDir, err)
	}
	if strings.Join(generatedFiles, "\n") != strings.Join(goldenFiles, "\n") {
		t.Fatalf("generated file list mismatch\ngolden:\n%s\ngenerated:\n%s", strings.Join(goldenFiles, "\n"), strings.Join(generatedFiles, "\n"))
	}
}

const apiBehaviorTest = `
import assert from "node:assert/strict"
import { DefaultApi, type FetchInterceptor } from "./api.ts"

void describe("api interceptors", () => {
  void test("passes the final intercepted request to a custom fetch", async () => {
    let fetchedRequest: Request | undefined
    const api = new DefaultApi({
      baseURL: "https://example.test",
      fetch: async request => {
        assert.ok(request instanceof Request)
        fetchedRequest = request
        return new Response("[]")
      },
      interceptors: [
        async chain => {
          const headers = new Headers(chain.request.headers)
          headers.set("traceparent", "custom-trace")
          return await chain.proceed(new Request(chain.request, { headers }))
        },
      ],
    })

    await api.listTestEmails()

    assert.ok(fetchedRequest instanceof Request)
    assert.equal(fetchedRequest.headers.get("traceparent"), "custom-trace")
  })

  void test("uses custom fetch for SSE reconnects", async () => {
    let fetchCalls = 0
    const encoder = new TextEncoder()
    const api = new DefaultApi({
      baseURL: "https://example.test",
      fetch: async () => {
        fetchCalls += 1
        return new Response(new ReadableStream({
          start(controller) {
            controller.enqueue(encoder.encode("data: {}\n\n"))
            controller.close()
          },
        }), { headers: { "content-type": "text/event-stream" } })
      },
      responseTimeoutMs: 0,
      sseMaxRetries: 1,
      sseReconnectBaseDelayMs: 0,
    })

    const response = await api.episodeProcessingEventsResult({
      episodeId: "episode_1",
      showId: "show_1",
      teamId: "team_1",
    })
    assert.equal(response.status, 200)
    if (response.status !== 200) throw new Error("unexpected status")
    const iterator = response.body[Symbol.asyncIterator]()
    assert.equal((await iterator.next()).done, false)
    assert.equal((await iterator.next()).done, false)
    assert.equal(fetchCalls, 2)
    await iterator.return?.()
  })

  void test("run in order and allow repeated proceed", async () => {
    const events: Array<string> = []
    const interceptors: Array<FetchInterceptor> = [
      async chain => {
        events.push("a:before")
        await chain.proceed()
        events.push("a:between")
        const response = await chain.proceed()
        events.push("a:after")
        return response
      },
      async chain => {
        events.push("b:before")
        const response = await chain.proceed()
        events.push("b:after")
        return response
      },
    ]
    const api = new DefaultApi({
      baseURL: "https://example.test",
      interceptors,
    })
    interceptors.push(async () => {
      events.push("fetch")
      return new Response("[]")
    })

    await api.listTestEmails()

    assert.deepEqual(events, [
      "a:before",
      "b:before",
      "fetch",
      "b:after",
      "a:between",
      "b:before",
      "fetch",
      "b:after",
      "a:after",
    ])
  })

  void test("may rewrite request headers", async () => {
    let header = ""
    const api = new DefaultApi({
      baseURL: "https://example.test",
      sseMaxRetries: 0,
      interceptors: [
        async chain => {
          const headers = new Headers(chain.request.headers)
          headers.set("x-test", "rewritten")
          return await chain.proceed(new Request(chain.request, { headers }))
        },
        async chain => {
          header = chain.request.headers.get("x-test") ?? ""
          return new Response("[]")
        },
      ],
    })

    await api.listTestEmails()

    assert.equal(header, "rewritten")
  })

  void test("may short-circuit with a synthetic response", async () => {
    let fetched = false
    const api = new DefaultApi({
      baseURL: "https://example.test",
      interceptors: [
        async () => new Response("[]"),
        async () => {
          fetched = true
          return new Response("network")
        },
      ],
    })

    const response = await api.listTestEmails()

    assert.deepEqual(response, [])
    assert.equal(fetched, false)
  })

  void test("does not consume SSE response bodies", async () => {
    const api = new DefaultApi({
      baseURL: "https://example.test",
      interceptors: [
        async () => new Response("data: still-readable\n\n", {
          headers: { "content-type": "text/event-stream" },
        }),
      ],
    })

    const response = await api.episodeProcessingEventsResult({
      episodeId: "episode_1",
      showId: "show_1",
      teamId: "team_1",
    })

    assert.equal(response.status, 200)
    const reader = response.raw.body?.getReader()
    assert.ok(reader)
    const chunk = await reader.read()
    assert.equal(new TextDecoder().decode(chunk.value), "data: still-readable\n\n")
    await reader.cancel()
  })

  void test("does not reconnect after a clean SSE stream end when disabled", async () => {
    let requests = 0
    const encoder = new TextEncoder()
    const api = new DefaultApi({
      baseURL: "https://example.test",
      responseTimeoutMs: 0,
      sseReconnectOnStreamEnd: false,
      interceptors: [
        async () => {
          requests += 1
          return new Response(new ReadableStream({
            start(controller) {
              controller.enqueue(encoder.encode(
                "event: rss.import.progress\ndata: {\"type\":\"terminal\",\"status\":\"cancelled\"}\n\n",
              ))
              controller.close()
            },
          }), { headers: { "content-type": "text/event-stream" } })
        },
      ],
    })
    const response = await api.episodeProcessingEventsResult({
      episodeId: "episode_1",
      showId: "show_1",
      teamId: "team_1",
    })
    assert.equal(response.status, 200)
    if (response.status !== 200) throw new Error("unexpected status")
    const iterator = response.body[Symbol.asyncIterator]()
    assert.equal((await iterator.next()).done, false)
    assert.equal((await iterator.next()).done, true)
    assert.equal(requests, 1)
  })

  void test("bounds ordinary response body decoding", async () => {
    const api = new DefaultApi({
      baseURL: "https://example.test",
      responseTimeoutMs: 20,
      interceptors: [
        async chain => new Response(new ReadableStream({
          start(controller) {
            chain.request.signal.addEventListener("abort", () => {
              controller.error(chain.request.signal.reason)
            }, { once: true })
          },
        }), { headers: { "content-type": "application/json" } }),
      ],
    })

    await assert.rejects(api.listTestEmails(), { name: "TimeoutError" })
  })

  void test("bounds SSE connection establishment but not total lifetime", async () => {
    const establishmentTimeout = new DefaultApi({
      baseURL: "https://example.test",
      responseTimeoutMs: 20,
      sseMaxRetries: 0,
      interceptors: [
        async chain => await new Promise((_resolve, reject) => {
          chain.request.signal.addEventListener("abort", () => {
            reject(chain.request.signal.reason)
          }, { once: true })
        }),
      ],
    })
    await assert.rejects(establishmentTimeout.episodeProcessingEventsResult({
      episodeId: "episode_1",
      showId: "show_1",
      teamId: "team_1",
    }), { name: "TimeoutError" })

    const encoder = new TextEncoder()
    let eventTimer: ReturnType<typeof setTimeout> | undefined
    const streaming = new DefaultApi({
      baseURL: "https://example.test",
      responseTimeoutMs: 20,
      sseIdleTimeoutMs: 1_000,
      interceptors: [
        async () => new Response(new ReadableStream({
          cancel() {
            if (eventTimer !== undefined) clearTimeout(eventTimer)
          },
          start(controller) {
            eventTimer = setTimeout(() => {
              controller.enqueue(encoder.encode("data: {}\n\n"))
            }, 40)
          },
        }), { headers: { "content-type": "text/event-stream" } }),
      ],
    })
    const response = await streaming.episodeProcessingEventsResult({
      episodeId: "episode_1",
      showId: "show_1",
      teamId: "team_1",
    })
    assert.equal(response.status, 200)
    if (response.status !== 200) throw new Error("unexpected status")
    const iterator = response.body[Symbol.asyncIterator]()
    assert.equal((await iterator.next()).done, false)
    await iterator.return?.()
  })

  void test("SSE idle timeout and early iterator return cancel the body", async () => {
    let idleCanceled = false
    const idleApi = new DefaultApi({
      baseURL: "https://example.test",
      responseTimeoutMs: 0,
      sseIdleTimeoutMs: 20,
      sseMaxRetries: 0,
      interceptors: [
        async () => new Response(new ReadableStream({
          cancel() {
            idleCanceled = true
          },
        }), { headers: { "content-type": "text/event-stream" } }),
      ],
    })
    const idleResponse = await idleApi.episodeProcessingEventsResult({
      episodeId: "episode_1",
      showId: "show_1",
      teamId: "team_1",
    })
    assert.equal(idleResponse.status, 200)
    if (idleResponse.status !== 200) throw new Error("unexpected status")
    await assert.rejects(idleResponse.body[Symbol.asyncIterator]().next(), { name: "TimeoutError" })
    assert.equal(idleCanceled, true)

    let earlyCanceled = false
    const earlyApi = new DefaultApi({
      baseURL: "https://example.test",
      responseTimeoutMs: undefined,
      sseIdleTimeoutMs: undefined,
      interceptors: [
        async () => new Response(new ReadableStream({
          start(controller) {
            controller.enqueue(new TextEncoder().encode("data: {}\n\n"))
          },
          cancel() {
            earlyCanceled = true
          },
        }), { headers: { "content-type": "text/event-stream" } }),
      ],
    })
    const earlyResponse = await earlyApi.episodeProcessingEventsResult({
      episodeId: "episode_1",
      showId: "show_1",
      teamId: "team_1",
    })
    assert.equal(earlyResponse.status, 200)
    if (earlyResponse.status !== 200) throw new Error("unexpected status")
    const earlyIterator = earlyResponse.body[Symbol.asyncIterator]()
    await earlyIterator.next()
    await earlyIterator.return?.()
    assert.equal(earlyCanceled, true)
  })

  void test("SSE heartbeat chunks reset idle timeout", async () => {
    const encoder = new TextEncoder()
    const api = new DefaultApi({
      baseURL: "https://example.test",
      responseTimeoutMs: 0,
      sseIdleTimeoutMs: 25,
      interceptors: [
        async () => new Response(new ReadableStream({
          start(controller) {
            setTimeout(() => controller.enqueue(encoder.encode(": heartbeat\n\n")), 10)
            setTimeout(() => controller.enqueue(encoder.encode(": heartbeat\n\n")), 20)
            setTimeout(() => controller.enqueue(encoder.encode("data: {}\n\n")), 30)
          },
        }), { headers: { "content-type": "text/event-stream" } }),
      ],
    })
    const response = await api.episodeProcessingEventsResult({
      episodeId: "episode_1",
      showId: "show_1",
      teamId: "team_1",
    })
    assert.equal(response.status, 200)
    if (response.status !== 200) throw new Error("unexpected status")
    const iterator = response.body[Symbol.asyncIterator]()
    assert.equal((await iterator.next()).done, false)
    await iterator.return?.()
  })

  void test("caller abort signal remains authoritative", async () => {
    const caller = new AbortController()
    const reason = new Error("caller canceled")
    const api = new DefaultApi({
      baseURL: "https://example.test",
      responseTimeoutMs: 1_000,
      interceptors: [
        async chain => await new Promise((_resolve, reject) => {
          chain.request.signal.addEventListener("abort", () => {
            reject(chain.request.signal.reason)
          }, { once: true })
        }),
      ],
    })
    const pending = api.listTestEmails({ signal: caller.signal })
    caller.abort(reason)
    await assert.rejects(pending, error => error === reason)
  })
})
`

const typescriptQueryBehaviorTest = `
import assert from "node:assert/strict"
import { DefaultApi } from "./api.ts"

void describe("TypeScript client queries", () => {
  void test("encodes scalar and repeated values", () => {
    const api = new DefaultApi({ baseURL: "https://example.test" })
    const request = api.createThingRequest({
      thingId: "a/b",
      tag: "red blue&green",
      notify: true,
      label: ["alpha beta", "x&y"],
      xRequestId: "request/1",
      createThing: { name: "fixture" },
    })
    const url = new URL(request.url)

    assert.equal(url.pathname, "/things/a%2Fb")
    assert.equal(url.searchParams.get("tag"), "red blue&green")
    assert.equal(url.searchParams.get("notify"), "true")
    assert.deepEqual(url.searchParams.getAll("label"), ["alpha beta", "x&y"])
  })
})
`

const goClientBehaviorTest = `package publicapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func bearerRequestEditor(token string) RequestEditorFn {
	return func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
}

func TestRequestEditorsAndResponses(t *testing.T) {
	var events []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != "/things/a%2Fb" {
			t.Errorf("escaped path = %q", request.URL.EscapedPath())
		}
		if request.URL.Query().Get("tag") != "red blue&green" {
			t.Errorf("tag = %q", request.URL.Query().Get("tag"))
		}
		if request.URL.Query().Get("notify") != "true" {
			t.Errorf("notify = %q", request.URL.Query().Get("notify"))
		}
		labels := request.URL.Query()["label"]
		if len(labels) != 2 || labels[0] != "alpha beta" || labels[1] != "x&y" {
			t.Errorf("labels = %q", labels)
		}
		if request.Header.Get("x-request-id") != "request/1" {
			t.Errorf("x-request-id = %q", request.Header.Get("x-request-id"))
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("Authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("x-before") != "yes" {
			t.Errorf("x-before = %q", request.Header.Get("x-before"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != ` + "`" + `{"name":"fixture"}` + "`" + ` {
			t.Errorf("body = %s", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(` + "`" + `{"id":"thing_1","name":"fixture"}` + "`" + `))
	}))
	defer server.Close()

	outer := func(_ context.Context, request *http.Request) error {
		events = append(events, "outer")
		request.Header.Set("x-before", "yes")
		return nil
	}
	inner := func(_ context.Context, _ *http.Request) error {
		events = append(events, "inner")
		return nil
	}
	client, err := NewClient(
		ClientOptions{BaseURL: server.URL},
		WithRequestEditorFn(outer),
		WithRequestEditorFn(inner),
		WithRequestEditorFn(bearerRequestEditor("secret")),
	)
	if err != nil {
		t.Fatal(err)
	}
	tag := "red blue&green"
	labels := []string{"alpha beta", "x&y"}
	response, err := client.CreateThing(context.Background(), CreateThingParams{
		ThingId: "a/b",
		Tag: &tag,
		Notify: true,
		Label: &labels,
		XRequestId: "request/1",
		Body: CreateThing{Name: "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status201 == nil || response.Status201.Id != "thing_1" {
		t.Fatalf("typed response = %#v", response.Status201)
	}
	want := []string{"outer", "inner"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("request editor order = %v", events)
	}
}

func TestRawRequestBodyAndSharedPathParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.EscapedPath() != "/uploads/channel%2Fone" {
			t.Errorf("escaped path = %q", request.URL.EscapedPath())
		}
		if request.URL.Query().Get("uploadType") != "multipart" {
			t.Errorf("uploadType = %q", request.URL.Query().Get("uploadType"))
		}
		if request.Header.Get("Content-Type") != "video/mp4" {
			t.Errorf("Content-Type = %q", request.Header.Get("Content-Type"))
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(body) != "media" {
			t.Errorf("body = %q", body)
		}
		writer.Header().Set("Content-Type", "application/json")
		writer.WriteHeader(http.StatusCreated)
		_, _ = writer.Write([]byte(` + "`" + `{"id":"video_1","name":"episode"}` + "`" + `))
	}))
	defer server.Close()

	client, err := NewClient(ClientOptions{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.UploadMedia(context.Background(), UploadMediaParams{
		Owner: "channel/one", UploadType: "multipart", Body: strings.NewReader("media"), ContentType: "video/mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.Status201 == nil || response.Status201.Id != "video_1" {
		t.Fatalf("typed response = %#v", response.Status201)
	}
}

func TestRequestEditorFailurePreventsTransportExecution(t *testing.T) {
	sentinel := errors.New("editor failed")
	transportCalls := 0
	laterEditorRan := false
	client, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithHTTPClient(HTTPClientFunc(func(*http.Request) (*http.Response, error) {
			transportCalls++
			return nil, errors.New("transport must not run")
		})),
		WithRequestEditorFn(func(context.Context, *http.Request) error {
			return sentinel
		}),
		WithRequestEditorFn(func(context.Context, *http.Request) error {
			laterEditorRan = true
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.CreateThing(context.Background(), CreateThingParams{
		ThingId: "one", XRequestId: "request", Body: CreateThing{Name: "name"},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("request editor error = %v", err)
	}
	if transportCalls != 0 || laterEditorRan {
		t.Fatalf("after failed editor: transport calls = %d, later editor = %v", transportCalls, laterEditorRan)
	}
}

func TestSingleTransportExecutionAndUnexpectedStatus(t *testing.T) {
	editorCalls := 0
	transportCalls := 0
	unexpected, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithRequestEditorFn(func(context.Context, *http.Request) error {
			editorCalls++
			return nil
		}),
		WithHTTPClient(HTTPClientFunc(func(*http.Request) (*http.Response, error) {
			transportCalls++
			return &http.Response{
				StatusCode: http.StatusTeapot,
				Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("diagnostic")),
			}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = unexpected.WatchEvents(context.Background())
	var statusErr *UnexpectedStatusError
	if !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusTeapot || statusErr.Body != "diagnostic" {
		t.Fatalf("unexpected status error = %#v", err)
	}
	if editorCalls != 1 || transportCalls != 1 {
		t.Fatalf("editor calls = %d, transport calls = %d", editorCalls, transportCalls)
	}
}

type trackedBody struct {
	io.Reader
	mu sync.Mutex
	closed bool
}

func (body *trackedBody) Close() error {
	body.mu.Lock()
	body.closed = true
	body.mu.Unlock()
	return nil
}

func (body *trackedBody) isClosed() bool {
	body.mu.Lock()
	defer body.mu.Unlock()
	return body.closed
}

type tinyChunkReader struct {
	data string
}

func (reader *tinyChunkReader) Read(buffer []byte) (int, error) {
	if reader.data == "" {
		return 0, io.EOF
	}
	buffer[0] = reader.data[0]
	reader.data = reader.data[1:]
	return 1, nil
}

func TestSSEChunkingHeartbeatsCRLFMultilineFinalEOFAndNestedOneOf(t *testing.T) {
	body := &trackedBody{Reader: &tinyChunkReader{data: ": heartbeat\r\n\r\nevent: progress\r\ndata: {\"kind\":\"progress\",\r\ndata: \"percent\":7}\r\n\r\nevent: terminal\ndata: {\"kind\":\"terminal\",\"result\":{\"status\":\"completed\",\"thing\":{\"id\":\"thing_1\",\"name\":\"done\"}}}"}}
	stream := NewSSEStream[Event](body)
	first, ok, err := stream.Next(context.Background())
	if err != nil || !ok || first.ProgressEvent == nil || first.ProgressEvent.Percent != 7 || stream.EventName() != "progress" {
		t.Fatalf("first event = %#v, %v, %v, %q", first, ok, err, stream.EventName())
	}
	second, ok, err := stream.Next(context.Background())
	if err != nil || !ok || second.TerminalEvent == nil || stream.EventName() != "terminal" {
		t.Fatalf("second event = %#v, %v, %v, %q", second, ok, err, stream.EventName())
	}
	completed, ok := second.TerminalEvent.Result.GetActualInstance().(*CompletedResult)
	if !ok || completed.Thing.Id != "thing_1" {
		t.Fatalf("nested terminal = %#v", second.TerminalEvent.Result)
	}
	_, ok, err = stream.Next(context.Background())
	if err != nil || ok || !body.isClosed() {
		t.Fatalf("stream EOF = %v, %v, closed=%v", ok, err, body.isClosed())
	}
}

func TestSSEMalformedJSONAndCallerClose(t *testing.T) {
	malformedBody := &trackedBody{Reader: strings.NewReader("data: {bad}\n\n")}
	stream := NewSSEStream[Event](malformedBody)
	_, _, err := stream.Next(context.Background())
	if err == nil || !malformedBody.isClosed() {
		t.Fatalf("malformed SSE error = %v, closed=%v", err, malformedBody.isClosed())
	}
	callerBody := &trackedBody{Reader: strings.NewReader("")}
	callerStream := NewSSEStream[Event](callerBody)
	if err := callerStream.Close(); err != nil || !callerBody.isClosed() {
		t.Fatalf("caller close = %v, closed=%v", err, callerBody.isClosed())
	}
}

func TestSSECancellationClosesBody(t *testing.T) {
	reader, writer := io.Pipe()
	defer writer.Close()
	stream := NewSSEStream[Event](reader)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, err := stream.Next(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation error = %v", err)
	}
}

type contextBody struct {
	ctx context.Context
}

func (body *contextBody) Read([]byte) (int, error) {
	<-body.ctx.Done()
	return 0, context.Cause(body.ctx)
}

func (*contextBody) Close() error { return nil }

func TestResponseTimeoutBoundsHeadersAndOrdinaryBody(t *testing.T) {
	parameters := CreateThingParams{
		ThingId: "one", XRequestId: "request", Body: CreateThing{Name: "name"},
	}
	headersClient, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithResponseTimeout(20*time.Millisecond),
		WithHTTPClient(HTTPClientFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, context.Cause(request.Context())
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = headersClient.CreateThing(context.Background(), parameters)
	var timeoutErr *ResponseTimeoutError
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("response header timeout error = %T %v", err, err)
	}

	bodyClient, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithResponseTimeout(20*time.Millisecond),
		WithHTTPClient(HTTPClientFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header: make(http.Header),
				Body: &contextBody{ctx: request.Context()},
			}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = bodyClient.CreateThing(context.Background(), parameters)
	if !errors.As(err, &timeoutErr) {
		t.Fatalf("response body timeout error = %T %v", err, err)
	}
}

func TestSSETimeoutsUseHeadersThenChunkIdle(t *testing.T) {
	reader, writer := io.Pipe()
	client, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithResponseTimeout(20*time.Millisecond),
		WithSSEIdleTimeout(200*time.Millisecond),
		WithHTTPClient(HTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: reader}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.WatchEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(40 * time.Millisecond)
		_, _ = writer.Write([]byte("data: {\"kind\":\"progress\",\"percent\":1}\n\n"))
	}()
	event, ok, err := response.Status200.Next(context.Background())
	if err != nil || !ok || event.ProgressEvent == nil {
		t.Fatalf("SSE outlived response establishment timeout = %#v, %v, %v", event, ok, err)
	}
	_ = response.Status200.Close()
	_ = writer.Close()

	idleReader, idleWriter := io.Pipe()
	idleClient, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithResponseTimeout(0),
		WithSSEIdleTimeout(20*time.Millisecond),
		WithSSEMaxRetries(0),
		WithHTTPClient(HTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: idleReader}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	idleResponse, err := idleClient.WatchEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = idleResponse.Status200.Next(context.Background())
	var idleErr *SSEIdleTimeoutError
	if !errors.As(err, &idleErr) {
		t.Fatalf("SSE idle timeout error = %T %v", err, err)
	}
	_ = idleWriter.Close()
}

func TestSSEHeartbeatChunksResetIdleTimeout(t *testing.T) {
	reader, writer := io.Pipe()
	client, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithResponseTimeout(0),
		WithSSEIdleTimeout(25*time.Millisecond),
		WithHTTPClient(HTTPClientFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: reader}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.WatchEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range 2 {
			time.Sleep(10 * time.Millisecond)
			_, _ = writer.Write([]byte(": heartbeat\n\n"))
		}
		time.Sleep(10 * time.Millisecond)
		_, _ = writer.Write([]byte("data: {\"kind\":\"progress\",\"percent\":1}\n\n"))
	}()
	event, ok, err := response.Status200.Next(context.Background())
	if err != nil || !ok || event.ProgressEvent == nil {
		t.Fatalf("heartbeat-reset SSE event = %#v, %v, %v", event, ok, err)
	}
	_ = response.Status200.Close()
	_ = writer.Close()
}

func TestSSECleanEOFCanDisableReconnect(t *testing.T) {
	requests := 0
	client, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithResponseTimeout(0),
		WithSSEReconnectOnStreamEnd(false),
		WithHTTPClient(HTTPClientFunc(func(*http.Request) (*http.Response, error) {
			requests++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader("data: {\"kind\":\"progress\",\"percent\":1}\n\n")),
			}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.WatchEvents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	event, ok, err := response.Status200.Next(context.Background())
	if err != nil || !ok || event.ProgressEvent == nil {
		t.Fatalf("clean EOF first event = %#v, %v, %v", event, ok, err)
	}
	_, ok, err = response.Status200.Next(context.Background())
	if err != nil || ok || requests != 1 {
		t.Fatalf("clean EOF = %v, %v, requests=%d", err, ok, requests)
	}
}

type requestEditorContextKey struct{}

func TestRequestEditorContextSupportsTraceInjection(t *testing.T) {
	const traceValue = "trace-from-operation-context"
	editorSawRequestContext := false
	transportTraceHeader := ""
	client, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithRequestEditorFn(func(ctx context.Context, request *http.Request) error {
			value, _ := ctx.Value(requestEditorContextKey{}).(string)
			editorSawRequestContext = request.Context().Value(requestEditorContextKey{}) == traceValue
			request.Header.Set("traceparent", value)
			return nil
		}),
		WithHTTPClient(HTTPClientFunc(func(request *http.Request) (*http.Response, error) {
			transportTraceHeader = request.Header.Get("traceparent")
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header: make(http.Header),
				Body: io.NopCloser(strings.NewReader(` + "`" + `{"id":"thing_1","name":"fixture"}` + "`" + `)),
			}, nil
		})),
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), requestEditorContextKey{}, traceValue)
	_, err = client.CreateThing(ctx, CreateThingParams{
		ThingId: "one", XRequestId: "request", Body: CreateThing{Name: "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !editorSawRequestContext || transportTraceHeader != traceValue {
		t.Fatalf("context propagation = %v, trace header = %q", editorSawRequestContext, transportTraceHeader)
	}
}

func TestCallerContextRemainsAuthoritative(t *testing.T) {
	sentinel := errors.New("caller canceled")
	ctx, cancel := context.WithCancelCause(context.Background())
	cancel(sentinel)
	client, err := NewClient(
		ClientOptions{BaseURL: "https://example.test"},
		WithResponseTimeout(time.Second),
		WithHTTPClient(HTTPClientFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, context.Cause(request.Context())
		})),
		WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			detached := request.Clone(context.Background())
			*request = *detached
			return nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.WatchEvents(ctx)
	if !errors.Is(err, sentinel) {
		t.Fatalf("caller cancellation error = %v", err)
	}
}

`
