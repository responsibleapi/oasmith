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

func TestCLIGeneratesInsideIgnoredDirectoriesWithoutExternalTools(t *testing.T) {
	t.Parallel()

	binary := filepath.Join(t.TempDir(), "oasmith")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, "./cmd/oasmith")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build generator: %v\n%s", err, output)
	}
	for _, testCase := range []struct {
		name    string
		lang    string
		fixture string
		golden  string
	}{
		{name: "Go", lang: "go", fixture: "public-client.yaml", golden: "public-client-go"},
		{name: "TypeScript", lang: "typescript", fixture: "private.yaml", golden: "private-typescript"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			workspace := t.TempDir()
			init := exec.CommandContext(t.Context(), "git", "init", "-q", workspace)
			if output, err := init.CombinedOutput(); err != nil {
				t.Fatalf("initialize caller repository: %v\n%s", err, output)
			}
			for name, content := range map[string]string{
				".gitignore":      "gen/\n",
				".prettierignore": "*.ts\n",
			} {
				if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0o644); err != nil {
					t.Fatalf("write caller %s: %v", name, err)
				}
			}
			workingDir := filepath.Join(workspace, "packages", "openapi")
			if err := os.MkdirAll(workingDir, 0o750); err != nil {
				t.Fatalf("create nested working directory: %v", err)
			}
			fixture, err := filepath.Abs(filepath.Join("testdata", "fixtures", testCase.fixture))
			if err != nil {
				t.Fatalf("resolve fixture: %v", err)
			}
			command := exec.CommandContext(t.Context(), binary,
				"--openapi", fixture, "--mode", "client", "--lang", testCase.lang,
				"--out", "../../gen/client",
			)
			command.Dir = workingDir
			command.Env = append(os.Environ(), "PATH="+t.TempDir())
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("generate ignored %s client: %v\n%s", testCase.lang, err, output)
			}
			compareDirs(t, filepath.Join("testdata", "golden", testCase.golden), filepath.Join(workspace, "gen", "client"))
			entries, err := os.ReadDir(workingDir)
			if err != nil {
				t.Fatalf("read caller working directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("generator left files in caller working directory: %v", entries)
			}
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
	if !strings.Contains(string(raw), `encodeURIComponent(requestParameters['thingId'])`) {
		t.Fatal("generated TypeScript API does not directly escape path parameters")
	}
	if strings.Contains(string(raw), "encodeURIComponent(String(") {
		t.Fatal("generated TypeScript API unnecessarily converts path parameters")
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
		"go.mod":         "module generatedclient\n\ngo 1.27.0\n",
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

func TestGeneratedClientsUseCanonicalRequestAndResponseShapes(t *testing.T) {
	t.Parallel()

	fixturePath := filepath.Join("testdata", "fixtures", "public-client.yaml")
	doc, err := openapi.ParseFile(fixturePath)
	if err != nil {
		t.Fatalf("parse public client fixture: %v", err)
	}
	goOut := t.TempDir()
	if err := goemit.EmitClient(doc, goemit.Options{OutDir: goOut, SourcePath: fixturePath}); err != nil {
		t.Fatalf("emit Go client: %v", err)
	}
	goRaw, err := os.ReadFile(filepath.Join(goOut, "client.go"))
	if err != nil {
		t.Fatalf("read Go client: %v", err)
	}
	goSource := string(goRaw)
	for _, want := range []string{
		"requestBody := bytes.NewReader(encodedBody)",
		"case 401, 403:",
	} {
		if !strings.Contains(goSource, want) {
			t.Fatalf("generated Go client missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"Status401 bool",
		"Status403 bool",
	} {
		if strings.Contains(goSource, forbidden) {
			t.Fatalf("generated Go client contains redundant bodyless response field %q", forbidden)
		}
	}

	typeScriptOut := t.TempDir()
	if err := tsemit.Emit(doc, tsemit.Options{OutDir: typeScriptOut}); err != nil {
		t.Fatalf("emit TypeScript client: %v", err)
	}
	typeScriptRaw, err := os.ReadFile(filepath.Join(typeScriptOut, "api.ts"))
	if err != nil {
		t.Fatalf("read TypeScript client: %v", err)
	}
	typeScriptSource := string(typeScriptRaw)
	if !strings.Contains(typeScriptSource, "body: CreateThing;\n") {
		t.Fatal("generated TypeScript client does not expose the canonical body request parameter")
	}
	if strings.Contains(typeScriptSource, "createThing: CreateThing;\n") {
		t.Fatal("generated TypeScript client exposes a schema-derived request body parameter")
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

func TestOperationIDCannotInventWireBehavior(t *testing.T) {
	t.Parallel()

	doc, err := openapi.Parse([]byte(`openapi: 3.2.0
info:
  title: Operation identity fixture
  version: "1"
paths:
  /declared-upload:
    post:
      operationId: youtube.videos.insert
      requestBody:
        required: true
        content:
          application/octet-stream: {}
      responses:
        "204":
          description: Accepted
`))
	if err != nil {
		t.Fatalf("parse operation identity fixture: %v", err)
	}
	outDir := t.TempDir()
	if err := goemit.EmitClient(doc, goemit.Options{OutDir: outDir, SourcePath: "identity.yaml"}); err != nil {
		t.Fatalf("emit operation identity fixture: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "client.go"))
	if err != nil {
		t.Fatalf("read generated operation identity client: %v", err)
	}
	source := string(raw)
	if !strings.Contains(source, `path := "/declared-upload"`) {
		t.Fatalf("generated client omitted declared path:\n%s", source)
	}
	for _, forbidden := range []string{
		"/upload/youtube/v3/videos",
		"UploadBaseURL",
		"Resumable",
		"Content-Range",
		"Range header",
		"StatusCode == 308",
	} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("operationId invented %q behavior:\n%s", forbidden, source)
		}
	}
}

func TestTypeScriptOperationIDCannotInventWireBehavior(t *testing.T) {
	t.Parallel()

	var sources []string
	for _, operationID := range []string{"uploadMedia", "youtube.videos.insert"} {
		doc, err := openapi.Parse([]byte(strings.ReplaceAll(`openapi: 3.2.0
info:
  title: TypeScript operation identity fixture
  version: "1"
paths:
  /declared-upload:
    post:
      operationId: OPERATION_ID
      responses:
        "204":
          description: Accepted
`, "OPERATION_ID", operationID)))
		if err != nil {
			t.Fatalf("parse %s fixture: %v", operationID, err)
		}
		outDir := t.TempDir()
		if err := tsemit.Emit(doc, tsemit.Options{OutDir: outDir}); err != nil {
			t.Fatalf("emit %s fixture: %v", operationID, err)
		}
		raw, err := os.ReadFile(filepath.Join(outDir, "api.ts"))
		if err != nil {
			t.Fatalf("read %s generated API: %v", operationID, err)
		}
		source := string(raw)
		if !strings.Contains(source, `'/declared-upload'`) {
			t.Fatalf("%s omitted declared path:\n%s", operationID, source)
		}
		for _, forbidden := range []string{"/upload/youtube/v3/videos", "Content-Range", "resumable"} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s invented %q behavior:\n%s", operationID, forbidden, source)
			}
		}
		sources = append(sources, source)
	}

	normalizedDotted := strings.NewReplacer(
		"YoutubeVideosInsert", "UploadMedia",
		"youtubeVideosInsert", "uploadMedia",
	).Replace(sources[1])
	if sources[0] != normalizedDotted {
		t.Fatal("changing only operationId changed generated wire behavior")
	}
}

func TestGoClientRequiresBaseURLAndGeneratesDeclaredSequentialMultipart(t *testing.T) {
	t.Parallel()

	doc, err := openapi.Parse([]byte(`openapi: 3.2.0
info:
  title: Ordered upload fixture
  version: "1"
servers:
  - url: https://api.example.test
paths:
  /declared-upload:
    post:
      operationId: uploadMedia
      servers:
        - url: https://upload.example.test
      parameters:
        - name: uploadType
          in: query
          required: true
          schema:
            type: string
            const: multipart
      requestBody:
        required: true
        content:
          multipart/related:
            schema:
              type: array
              minItems: 2
              maxItems: 2
              prefixItems:
                - title: metadata
                  $ref: "#/components/schemas/Video"
                - title: media
                  type: string
                  format: binary
            prefixEncoding:
              - contentType: application/json
              - contentType: video/*,application/octet-stream
      responses:
        "201":
          description: Uploaded
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Video"
components:
  schemas:
    Video:
      type: object
      required: [id]
      properties:
        id:
          type: string
`))
	if err != nil {
		t.Fatalf("parse ordered multipart fixture: %v", err)
	}
	outDir := t.TempDir()
	if err := goemit.EmitClient(doc, goemit.Options{OutDir: outDir, SourcePath: "ordered.yaml"}); err != nil {
		t.Fatalf("emit ordered multipart fixture: %v", err)
	}
	clientSource, err := os.ReadFile(filepath.Join(outDir, "client.go"))
	if err != nil {
		t.Fatalf("read generated ordered multipart client: %v", err)
	}
	for _, forbidden := range []string{
		"baseURLOverride",
		"DefaultServerURL",
		"https://api.example.test",
		"https://upload.example.test",
	} {
		if strings.Contains(string(clientSource), forbidden) {
			t.Fatalf("generated Go client contains %q", forbidden)
		}
	}
	for name, source := range map[string]string{
		"go.mod":         "module orderedclient\n\ngo 1.27.0\n",
		"client_test.go": sequentialMultipartBehaviorTest,
	} {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(source), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = outDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("generated ordered multipart client test failed: %v\n%s", err, output)
	}
}

func TestTypeScriptClientRequiresBaseURLAndGeneratesDeclaredSequentialMultipart(t *testing.T) {
	t.Parallel()

	doc, err := openapi.ParseFile(filepath.Join("testdata", "fixtures", "multipart.yaml"))
	if err != nil {
		t.Fatalf("parse TypeScript ordered multipart fixture: %v", err)
	}
	outDir := t.TempDir()
	if err := tsemit.Emit(doc, tsemit.Options{OutDir: outDir}); err != nil {
		t.Fatalf("emit TypeScript ordered multipart fixture: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "api.ts"))
	if err != nil {
		t.Fatalf("read generated TypeScript multipart client: %v", err)
	}
	for _, forbidden := range []string{
		"baseURLOverride",
		"https://youtube.googleapis.com",
		"https://www.googleapis.com",
		"Content-Range",
		"resumable",
		"StatusCode == 308",
		"Range header",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("generated TypeScript multipart client contains %q", forbidden)
		}
	}
}

func TestClientsRejectUnsupportedRequestBody(t *testing.T) {
	t.Parallel()

	doc, err := openapi.Parse([]byte(`openapi: 3.2.0
info:
  title: Unsupported request body fixture
  version: "1"
paths:
  /upload:
    post:
      operationId: upload
      requestBody:
        required: true
        content:
          multipart/form-data:
            schema:
              type: object
      responses:
        "204":
          description: Uploaded
`))
	if err != nil {
		t.Fatalf("parse unsupported request body fixture: %v", err)
	}
	for _, testCase := range []struct {
		name string
		emit func(string) error
	}{
		{
			name: "Go",
			emit: func(outDir string) error {
				return goemit.EmitClient(doc, goemit.Options{OutDir: outDir, SourcePath: "unsupported.yaml"})
			},
		},
		{
			name: "TypeScript",
			emit: func(outDir string) error {
				return tsemit.Emit(doc, tsemit.Options{OutDir: outDir})
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			err := testCase.emit(t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "operation upload has unsupported request body") {
				t.Fatalf("unsupported request body error = %v", err)
			}
		})
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

func TestOptionalRequestBodiesAndContentTypes(t *testing.T) {
	client, err := NewClient(ClientOptions{BaseURL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}

	absentJSON, err := client.NewPatchThingRequest(context.Background(), PatchThingParams{})
	if err != nil {
		t.Fatal(err)
	}
	if absentJSON.Body != nil || absentJSON.Header.Get("Content-Type") != "" {
		t.Fatalf("absent JSON body = %#v, Content-Type = %q", absentJSON.Body, absentJSON.Header.Get("Content-Type"))
	}
	body := CreateThing{Name: "patched"}
	presentJSON, err := client.NewPatchThingRequest(context.Background(), PatchThingParams{Body: &body})
	if err != nil {
		t.Fatal(err)
	}
	jsonBody, err := io.ReadAll(presentJSON.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(jsonBody) != ` + "`" + `{"name":"patched"}` + "`" + ` || presentJSON.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("present JSON body = %q, Content-Type = %q", jsonBody, presentJSON.Header.Get("Content-Type"))
	}

	absentRaw, err := client.NewUploadOptionalMediaRequest(context.Background(), UploadOptionalMediaParams{})
	if err != nil {
		t.Fatal(err)
	}
	if absentRaw.Body != nil || absentRaw.Header.Get("Content-Type") != "" {
		t.Fatalf("absent raw body = %#v, Content-Type = %q", absentRaw.Body, absentRaw.Header.Get("Content-Type"))
	}
	presentRaw, err := client.NewUploadOptionalMediaRequest(context.Background(), UploadOptionalMediaParams{
		Body: strings.NewReader("optional-media"),
	})
	if err != nil {
		t.Fatal(err)
	}
	rawBody, err := io.ReadAll(presentRaw.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(rawBody) != "optional-media" || presentRaw.Header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("present raw body = %q, Content-Type = %q", rawBody, presentRaw.Header.Get("Content-Type"))
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

const sequentialMultipartBehaviorTest = `package ordered

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestRequiredBaseURLAndOrderedParts(t *testing.T) {
	_, err := NewClient(ClientOptions{})
	if err == nil || !strings.Contains(err.Error(), "base URL must not be empty") {
		t.Fatalf("missing base URL error = %v", err)
	}

	request, err := NewClient(ClientOptions{BaseURL: "https://client.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	built, err := request.NewUploadMediaRequest(t.Context(), UploadMediaParams{
		UploadType: "multipart",
		Metadata: Video{Id: "metadata"},
		Media: strings.NewReader("complete-media"),
		MediaContentType: "video/mp4",
	})
	if err != nil {
		t.Fatal(err)
	}
	if built.URL.String() != "https://client.example.test/declared-upload?uploadType=multipart" {
		t.Fatalf("request URL = %q", built.URL)
	}
	mediaType, parameters, err := mime.ParseMediaType(built.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/related" || parameters["boundary"] == "" {
		t.Fatalf("content type = %q, %v", built.Header.Get("Content-Type"), err)
	}
	reader := multipart.NewReader(built.Body, parameters["boundary"])
	metadataPart, err := reader.NextPart()
	if err != nil || metadataPart.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("metadata part = %#v, %v", metadataPart, err)
	}
	var metadata Video
	if err := json.NewDecoder(metadataPart).Decode(&metadata); err != nil || metadata.Id != "metadata" {
		t.Fatalf("metadata = %#v, %v", metadata, err)
	}
	mediaPart, err := reader.NextPart()
	if err != nil || mediaPart.Header.Get("Content-Type") != "video/mp4" {
		t.Fatalf("media part = %#v, %v", mediaPart, err)
	}
	mediaBody, err := io.ReadAll(mediaPart)
	if err != nil || string(mediaBody) != "complete-media" {
		t.Fatalf("media = %q, %v", mediaBody, err)
	}
	if _, err := reader.NextPart(); err != io.EOF {
		t.Fatalf("extra multipart part: %v", err)
	}
	_, err = request.NewUploadMediaRequest(t.Context(), UploadMediaParams{
		UploadType: "resumable",
		Metadata: Video{Id: "metadata"},
		Media: http.NoBody,
		MediaContentType: "video/mp4",
	})
	if err == nil || !strings.Contains(err.Error(), "uploadType must be multipart") {
		t.Fatalf("invalid constant error = %v", err)
	}
}
`
