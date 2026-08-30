package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

type fakeLoginEntryHashRepository struct {
	loadValue string
	loadErr   error
	saveValue string
	saveErr   error

	loadCalls       int
	saveCalls       int
	savedCandidates []string
}

func (repository *fakeLoginEntryHashRepository) Load() (string, error) {
	repository.loadCalls++
	return repository.loadValue, repository.loadErr
}

func (repository *fakeLoginEntryHashRepository) SaveIfAbsent(candidate string) (string, error) {
	repository.saveCalls++
	repository.savedCandidates = append(repository.savedCandidates, candidate)
	if repository.saveErr != nil {
		return "", repository.saveErr
	}
	if repository.saveValue != "" {
		return repository.saveValue, nil
	}
	return candidate, nil
}

func TestGenerateLoginEntryHash(t *testing.T) {
	randomBytes := make([]byte, loginEntryHashByteLength)
	for index := range randomBytes {
		randomBytes[index] = byte(index)
	}

	got, err := generateLoginEntryHash(bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatalf("generateLoginEntryHash() returned error: %v", err)
	}
	const want = "000102030405060708090a0b0c0d0e0f"
	if got != want {
		t.Fatalf("generateLoginEntryHash() = %q, want %q", got, want)
	}
	if len(got) != loginEntryHashByteLength*2 {
		t.Fatalf("generated hash length = %d, want %d", len(got), loginEntryHashByteLength*2)
	}
}

func TestGenerateLoginEntryHashReturnsReadError(t *testing.T) {
	_, err := generateLoginEntryHash(strings.NewReader("too short"))
	if err == nil {
		t.Fatal("generateLoginEntryHash() expected a read error")
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("generateLoginEntryHash() error = %v, want io.ErrUnexpectedEOF", err)
	}
	if !strings.Contains(err.Error(), "生成随机登录入口失败") {
		t.Fatalf("generateLoginEntryHash() error = %q, want contextual message", err)
	}
}

func TestNormalizeLoginEntryHash(t *testing.T) {
	const lowerHash = "0123456789abcdef0123456789abcdef"
	tests := []struct {
		name    string
		value   string
		want    string
		wantErr bool
	}{
		{name: "empty value", value: "", want: ""},
		{name: "whitespace value", value: " \r\n\t", want: ""},
		{name: "valid lowercase", value: lowerHash, want: lowerHash},
		{
			name:  "uppercase with surrounding whitespace",
			value: " 0123456789ABCDEF0123456789ABCDEF\n",
			want:  lowerHash,
		},
		{name: "too short", value: lowerHash[:31], wantErr: true},
		{name: "too long", value: lowerHash + "0", wantErr: true},
		{name: "non hexadecimal", value: "g123456789abcdef0123456789abcdef", wantErr: true},
		{name: "path separator", value: "0123456789abcdef/123456789abcde", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeLoginEntryHash(test.value)
			if test.wantErr {
				if err == nil {
					t.Fatalf("normalizeLoginEntryHash(%q) expected an error", test.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeLoginEntryHash(%q) returned error: %v", test.value, err)
			}
			if got != test.want {
				t.Fatalf("normalizeLoginEntryHash(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestNormalizePersistedLoginEntryHashRejectsEmptyAndInvalidValues(t *testing.T) {
	for _, value := range []string{"", "   ", "invalid"} {
		t.Run(value, func(t *testing.T) {
			if _, err := normalizePersistedLoginEntryHash(value); err == nil {
				t.Fatalf("normalizePersistedLoginEntryHash(%q) expected an error", value)
			}
		})
	}
}

func TestSelectLoginEntryHashOverride(t *testing.T) {
	configValue := strings.Repeat("a", loginEntryHashByteLength*2)
	environmentValue := strings.Repeat("b", loginEntryHashByteLength*2)

	if got := selectLoginEntryHashOverride(configValue, environmentValue); got != environmentValue {
		t.Fatalf("environment override = %q, want %q", got, environmentValue)
	}
	if got := selectLoginEntryHashOverride(configValue, " \r\n\t"); got != configValue {
		t.Fatalf("blank environment override = %q, want config value %q", got, configValue)
	}
}

func TestResolveLoginEntryHash(t *testing.T) {
	const configuredUppercase = "ABCDEF0123456789ABCDEF0123456789"
	const configuredLowercase = "abcdef0123456789abcdef0123456789"

	t.Run("configured value has priority", func(t *testing.T) {
		repository := &fakeLoginEntryHashRepository{
			loadErr: errors.New("repository must not be called"),
			saveErr: errors.New("repository must not be called"),
		}

		got, err := resolveLoginEntryHash(" \t"+configuredUppercase+"\n", repository, strings.NewReader(""))
		if err != nil {
			t.Fatalf("resolveLoginEntryHash() returned error: %v", err)
		}
		if got != configuredLowercase {
			t.Fatalf("resolveLoginEntryHash() = %q, want %q", got, configuredLowercase)
		}
		if repository.loadCalls != 0 || repository.saveCalls != 0 {
			t.Fatalf("configured value accessed repository: load=%d save=%d", repository.loadCalls, repository.saveCalls)
		}
	})

	t.Run("persisted value is reused", func(t *testing.T) {
		repository := &fakeLoginEntryHashRepository{loadValue: configuredUppercase}

		got, err := resolveLoginEntryHash("", repository, strings.NewReader(""))
		if err != nil {
			t.Fatalf("resolveLoginEntryHash() returned error: %v", err)
		}
		if got != configuredLowercase {
			t.Fatalf("resolveLoginEntryHash() = %q, want %q", got, configuredLowercase)
		}
		if repository.loadCalls != 1 || repository.saveCalls != 0 {
			t.Fatalf("repository calls = load:%d save:%d, want load:1 save:0", repository.loadCalls, repository.saveCalls)
		}
	})

	t.Run("first generated value is persisted", func(t *testing.T) {
		repository := &fakeLoginEntryHashRepository{}
		randomBytes := bytes.Repeat([]byte{0xab}, loginEntryHashByteLength)
		want := strings.Repeat("ab", loginEntryHashByteLength)

		got, err := resolveLoginEntryHash("", repository, bytes.NewReader(randomBytes))
		if err != nil {
			t.Fatalf("resolveLoginEntryHash() returned error: %v", err)
		}
		if got != want {
			t.Fatalf("resolveLoginEntryHash() = %q, want %q", got, want)
		}
		if repository.loadCalls != 1 || repository.saveCalls != 1 {
			t.Fatalf("repository calls = load:%d save:%d, want load:1 save:1", repository.loadCalls, repository.saveCalls)
		}
		if len(repository.savedCandidates) != 1 || repository.savedCandidates[0] != want {
			t.Fatalf("saved candidates = %v, want [%s]", repository.savedCandidates, want)
		}
	})

	t.Run("concurrent winner returned by repository is used", func(t *testing.T) {
		winnerUppercase := strings.Repeat("CD", loginEntryHashByteLength)
		winnerLowercase := strings.ToLower(winnerUppercase)
		generated := strings.Repeat("11", loginEntryHashByteLength)
		repository := &fakeLoginEntryHashRepository{saveValue: winnerUppercase}

		got, err := resolveLoginEntryHash(
			"",
			repository,
			bytes.NewReader(bytes.Repeat([]byte{0x11}, loginEntryHashByteLength)),
		)
		if err != nil {
			t.Fatalf("resolveLoginEntryHash() returned error: %v", err)
		}
		if got != winnerLowercase {
			t.Fatalf("resolveLoginEntryHash() = %q, want concurrent winner %q", got, winnerLowercase)
		}
		if len(repository.savedCandidates) != 1 || repository.savedCandidates[0] != generated {
			t.Fatalf("saved candidates = %v, want generated candidate [%s]", repository.savedCandidates, generated)
		}
	})
}

func TestResolveLoginEntryHashPropagatesRepositoryErrors(t *testing.T) {
	loadErr := errors.New("load failed")
	if _, err := resolveLoginEntryHash("", &fakeLoginEntryHashRepository{loadErr: loadErr}, strings.NewReader("")); !errors.Is(err, loadErr) {
		t.Fatalf("resolveLoginEntryHash() load error = %v, want %v", err, loadErr)
	}

	saveErr := errors.New("save failed")
	repository := &fakeLoginEntryHashRepository{saveErr: saveErr}
	if _, err := resolveLoginEntryHash("", repository, bytes.NewReader(make([]byte, loginEntryHashByteLength))); !errors.Is(err, saveErr) {
		t.Fatalf("resolveLoginEntryHash() save error = %v, want %v", err, saveErr)
	}
}

func TestLoginEntryPath(t *testing.T) {
	const hash = "0123456789abcdef0123456789abcdef"
	if got, want := loginEntryPath(hash), "/"+hash; got != want {
		t.Fatalf("loginEntryPath(%q) = %q, want %q", hash, got, want)
	}
}

func TestFrontendRouteHandlerServesLoginEntry(t *testing.T) {
	const entryHash = "0123456789abcdef0123456789abcdef"
	frontend := frontendFixture()
	router := frontendTestRouter(frontend, loginEntryPath(entryHash))

	tests := []struct {
		name     string
		method   string
		target   string
		wantBody bool
	}{
		{name: "GET", method: http.MethodGet, target: "/" + entryHash, wantBody: true},
		{name: "GET with query", method: http.MethodGet, target: "/" + entryHash + "?from=bookmark", wantBody: true},
		{name: "HEAD", method: http.MethodHead, target: "/" + entryHash, wantBody: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performFrontendRequest(t, router, test.method, test.target)
			if response.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, want %d", test.method, test.target, response.Code, http.StatusOK)
			}
			if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
				t.Fatalf("%s %s Content-Type = %q, want text/html", test.method, test.target, contentType)
			}
			if test.wantBody && !strings.Contains(response.Body.String(), "INDEX_MARKER") {
				t.Fatalf("%s %s did not serve index.html: %q", test.method, test.target, response.Body.String())
			}
			if !test.wantBody && response.Body.Len() != 0 {
				t.Fatalf("%s %s body = %q, want empty HEAD body", test.method, test.target, response.Body.String())
			}
		})
	}
}

func TestFrontendRouteHandlerRejectsUnknownPages(t *testing.T) {
	const entryHash = "0123456789abcdef0123456789abcdef"
	entryPath := loginEntryPath(entryHash)
	router := frontendTestRouter(frontendFixture(), entryPath)

	tests := []struct {
		name   string
		method string
		target string
	}{
		{name: "root", method: http.MethodGet, target: "/"},
		{name: "unknown route", method: http.MethodGet, target: "/nodes"},
		{name: "wrong hash", method: http.MethodGet, target: "/ffffffffffffffffffffffffffffffff"},
		{name: "uppercase entry", method: http.MethodGet, target: strings.ToUpper(entryPath)},
		{name: "entry prefix", method: http.MethodGet, target: "/prefix" + entryPath},
		{name: "entry suffix", method: http.MethodGet, target: entryPath + "0"},
		{name: "entry with trailing slash", method: http.MethodGet, target: entryPath + "/"},
		{name: "direct index", method: http.MethodGet, target: "/index.html"},
		{name: "direct not found page", method: http.MethodGet, target: "/404.html"},
		{name: "other HTML file", method: http.MethodGet, target: "/help.html"},
		{name: "missing static resource", method: http.MethodGet, target: "/assets/missing.js"},
		{name: "POST entry", method: http.MethodPost, target: entryPath},
		{name: "PUT entry with query", method: http.MethodPut, target: entryPath + "?from=client"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performFrontendRequest(t, router, test.method, test.target)
			if response.Code != http.StatusNotFound {
				t.Fatalf("%s %s status = %d, want %d", test.method, test.target, response.Code, http.StatusNotFound)
			}
			body := response.Body.String()
			if strings.Contains(body, "INDEX_MARKER") {
				t.Fatalf("%s %s leaked index.html: %q", test.method, test.target, body)
			}
			if !strings.Contains(body, "NOT_FOUND_MARKER") {
				t.Fatalf("%s %s body = %q, want custom 404 page", test.method, test.target, body)
			}
		})
	}
}

func TestFrontendRouteHandlerServesExistingStaticAssets(t *testing.T) {
	const entryPath = "/0123456789abcdef0123456789abcdef"
	router := frontendTestRouter(frontendFixture(), entryPath)

	tests := []struct {
		name     string
		method   string
		target   string
		wantBody bool
	}{
		{name: "GET asset", method: http.MethodGet, target: "/assets/app.js", wantBody: true},
		{name: "GET asset with query", method: http.MethodGet, target: "/assets/app.js?v=1", wantBody: true},
		{name: "HEAD asset", method: http.MethodHead, target: "/assets/app.js", wantBody: false},
		{name: "favicon", method: http.MethodGet, target: "/favicon.svg", wantBody: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := performFrontendRequest(t, router, test.method, test.target)
			if response.Code != http.StatusOK {
				t.Fatalf("%s %s status = %d, want %d", test.method, test.target, response.Code, http.StatusOK)
			}
			if test.wantBody && !strings.Contains(response.Body.String(), "ASSET_MARKER") {
				t.Fatalf("%s %s body = %q, want static asset", test.method, test.target, response.Body.String())
			}
			if !test.wantBody && response.Body.Len() != 0 {
				t.Fatalf("%s %s body = %q, want empty HEAD body", test.method, test.target, response.Body.String())
			}
		})
	}
}

func TestFrontendRouteHandlerDoesNotListDirectories(t *testing.T) {
	router := frontendTestRouter(frontendFixture(), "/0123456789abcdef0123456789abcdef")
	for _, target := range []string{"/assets", "/assets/"} {
		t.Run(target, func(t *testing.T) {
			response := performFrontendRequest(t, router, http.MethodGet, target)
			if response.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want %d", target, response.Code, http.StatusNotFound)
			}
			if strings.Contains(response.Body.String(), "app.js") {
				t.Fatalf("GET %s exposed a directory listing: %q", target, response.Body.String())
			}
		})
	}
}

func TestFrontendRouteHandlerReturnsJSONForUnknownBackendPaths(t *testing.T) {
	router := frontendTestRouter(frontendFixture(), "/0123456789abcdef0123456789abcdef")
	for _, target := range []string{"/api/not-registered", "/sub/not-registered", "/shadowrocket/not-registered"} {
		t.Run(target, func(t *testing.T) {
			response := performFrontendRequest(t, router, http.MethodGet, target)
			if response.Code != http.StatusNotFound {
				t.Fatalf("unknown backend path status = %d, want %d", response.Code, http.StatusNotFound)
			}
			if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
				t.Fatalf("unknown backend path Content-Type = %q, want application/json", contentType)
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("unknown backend path body is not JSON: %v; body=%q", err, response.Body.String())
			}
			if body["error"] != "Not Found" {
				t.Fatalf("unknown backend path body = %#v, want error=Not Found", body)
			}
			if strings.Contains(response.Body.String(), "INDEX_MARKER") {
				t.Fatalf("unknown backend path leaked index.html: %q", response.Body.String())
			}
		})
	}
}

func TestFrontendRouteHandlerFallsBackWhen404PageIsMissing(t *testing.T) {
	frontend := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("INDEX_MARKER")},
	}
	router := frontendTestRouter(frontend, "/0123456789abcdef0123456789abcdef")
	response := performFrontendRequest(t, router, http.MethodGet, "/missing")

	if response.Code != http.StatusNotFound {
		t.Fatalf("missing route status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if got, want := response.Body.String(), "404 page not found"; got != want {
		t.Fatalf("missing route body = %q, want %q", got, want)
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("missing route Content-Type = %q, want text/html", contentType)
	}
}

func TestFrontendRouteHandlerReturnsEmptyHead404(t *testing.T) {
	router := frontendTestRouter(frontendFixture(), "/0123456789abcdef0123456789abcdef")
	response := performFrontendRequest(t, router, http.MethodHead, "/missing")

	if response.Code != http.StatusNotFound {
		t.Fatalf("HEAD missing route status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if response.Body.Len() != 0 {
		t.Fatalf("HEAD missing route body = %q, want empty body", response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("HEAD missing route Content-Type = %q, want text/html", contentType)
	}
}

func TestFrontendRouteHandlerReturns500WhenIndexIsMissing(t *testing.T) {
	frontend := fstest.MapFS{
		"404.html": &fstest.MapFile{Data: []byte("NOT_FOUND_MARKER")},
	}
	entryPath := "/0123456789abcdef0123456789abcdef"
	router := frontendTestRouter(frontend, entryPath)
	response := performFrontendRequest(t, router, http.MethodGet, entryPath)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("entry without index status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(response.Body.String(), "Embedded frontend index.html is missing") {
		t.Fatalf("entry without index body = %q, want diagnostic message", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "NOT_FOUND_MARKER") {
		t.Fatalf("entry without index unexpectedly served 404 page: %q", response.Body.String())
	}
}

func frontendFixture() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>INDEX_MARKER</html>")},
		"404.html":      &fstest.MapFile{Data: []byte("<html>NOT_FOUND_MARKER</html>")},
		"help.html":     &fstest.MapFile{Data: []byte("<html>PRIVATE_HTML_MARKER</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("ASSET_MARKER")},
		"favicon.svg":   &fstest.MapFile{Data: []byte("<svg>ASSET_MARKER</svg>")},
	}
}

func frontendTestRouter(frontend fstest.MapFS, entryPath string) http.Handler {
	router := gin.New()
	router.NoRoute(newFrontendRouteHandler(frontend, entryPath))
	return router
}

func performFrontendRequest(t *testing.T, router http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
