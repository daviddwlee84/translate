package mcpserver

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/daviddwlee84/translate/internal/appcore"
	"github.com/daviddwlee84/translate/internal/engine"
	"github.com/daviddwlee84/translate/internal/store"
)

// fakeService implements Service with canned data and captured inputs.
type fakeService struct {
	result     *engine.TranslateResult
	defineRes  *engine.TranslateResult
	recent     []store.Record
	lastParams appcore.Params
	lastSearch string
}

func (f *fakeService) Translate(_ context.Context, p appcore.Params) (*engine.TranslateResult, error) {
	f.lastParams = p
	return f.result, nil
}
func (f *fakeService) Define(_ context.Context, _ string) (*engine.TranslateResult, error) {
	return f.defineRes, nil
}
func (f *fakeService) HistoryRecent(_ context.Context, _ int) ([]store.Record, error) {
	return f.recent, nil
}
func (f *fakeService) HistorySearch(_ context.Context, q string, _ int) ([]store.Record, error) {
	f.lastSearch = q
	return f.recent, nil
}

// connect starts the MCP server over an in-memory transport and returns a
// connected client session. Both are torn down when the test ends.
func connect(t *testing.T, svc Service) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clientT, serverT := mcp.NewInMemoryTransports()
	srv := newServer(svc, "test")
	go func() { _ = srv.Run(ctx, serverT) }()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func firstText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *mcp.TextContent", res.Content[0])
	}
	return tc.Text
}

func TestMCPToolsList(t *testing.T) {
	session := connect(t, &fakeService{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	want := []string{"define", "history", "translate"}
	if len(names) != len(want) {
		t.Fatalf("tools = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("tools = %v, want %v", names, want)
		}
	}
}

func TestMCPCallTranslate(t *testing.T) {
	fake := &fakeService{result: &engine.TranslateResult{Translation: "你好", Target: "zh", Engine: "stub"}}
	session := connect(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "translate",
		Arguments: map[string]any{"text": "hello", "target": "zh"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected tool error: %s", firstText(t, res))
	}
	if got := firstText(t, res); got != "你好" {
		t.Fatalf("text content = %q, want 你好", got)
	}
	if fake.lastParams.Text != "hello" || fake.lastParams.Target != "zh" {
		t.Fatalf("params = %+v, want text=hello target=zh", fake.lastParams)
	}
	// Structured output carries the full result.
	sc, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structuredContent is %T, want object", res.StructuredContent)
	}
	if sc["translation"] != "你好" {
		t.Fatalf("structuredContent.translation = %v, want 你好", sc["translation"])
	}
}

func TestMCPCallDefine(t *testing.T) {
	fake := &fakeService{defineRes: &engine.TranslateResult{
		Dictionary: &engine.DictEntry{Word: "hello", Phonetic: "/həˈloʊ/"},
	}}
	session := connect(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "define",
		Arguments: map[string]any{"word": "hello"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got := firstText(t, res); got == "" || !strings.Contains(got, "hello") {
		t.Fatalf("define text = %q, want it to mention hello", got)
	}
}

func TestMCPCallHistorySearch(t *testing.T) {
	fake := &fakeService{recent: []store.Record{{Input: "hi", Output: "你好", SourceLang: "en", TargetLang: "zh"}}}
	session := connect(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "history",
		Arguments: map[string]any{"query": "hi", "limit": 5},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if fake.lastSearch != "hi" {
		t.Fatalf("search query = %q, want hi", fake.lastSearch)
	}
	if got := firstText(t, res); !strings.Contains(got, "你好") {
		t.Fatalf("history text = %q, want it to include 你好", got)
	}
}
