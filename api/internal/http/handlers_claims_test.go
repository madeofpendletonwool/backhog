package http

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

// The claims store's bar (MAD-466), on the same fixture every leak test
// stands: a three-chapter mystery with a mid-book reveal. The import must
// reject what does not cite (a quote that misses the text, a span that
// crosses chapters), the reads must clamp (a truth exists only once its
// evidence has been read; a version revealed past the position does not
// exist yet), and staleness must hide, not delete. The token gate mirrors
// the quiz one: a read-only token cannot import, a claims:write token can.

// mintTokenScopes mints a personal token carrying exactly the given
// scopes — the walk the settings page performs behind its checkboxes.
func mintTokenScopes(t *testing.T, base string, client *http.Client, scopes ...string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": "claims test", "scopes": scopes, "expires_days": 0})
	resp, err := client.Post(base+"/api/tokens", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("mint token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("mint token: status %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || out.Token == "" {
		t.Fatalf("mint token: decode %v (%q)", err, out.Token)
	}
	return out.Token
}

// postJSON is the cookie session's write helper.
func postJSON(t *testing.T, client *http.Client, url string, body any) (int, map[string]any) {
	t.Helper()
	payload, _ := json.Marshal(body)
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	if strings.Contains(resp.Header.Get("Content-Type"), "json") {
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &out)
	}
	return resp.StatusCode, out
}

// claimItem builds one import item over an exact canonical slice.
func claimItem(statement string, start, end int, quote string) map[string]any {
	return map[string]any{
		"statement": statement, "char_start": start, "char_end": end, "quote": quote,
	}
}

// claimsOf decodes a claims read into its rows.
func claimsOf(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	raw, _ := body["claims"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, c := range raw {
		out = append(out, c.(map[string]any))
	}
	return out
}

func statementsOf(claims []map[string]any) []string {
	out := make([]string, 0, len(claims))
	for _, c := range claims {
		out = append(out, c["statement"].(string))
	}
	return out
}

func hasStatement(claims []map[string]any, statement string) bool {
	for _, s := range statementsOf(claims) {
		if s == statement {
			return true
		}
	}
	return false
}

// TestImportClaimsCiteOrDrop: the deterministic door. A claim that cites
// the text lands; a quote that misses and a span that crosses a chapter
// are rejected per item with reasons, never fixed up; re-import is a
// no-op; and the import is the one POST the claims:write scope names.
func TestImportClaimsCiteOrDrop(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)

	_, textBody := app.get(t, "/api/books/"+epubFixtureEntry+"/text?until=none")
	text := textBody["text"].(string)

	valid := claimItem("the village slept under the hill", geo.alpha+6, geo.alpha+20, text[geo.alpha+6:geo.alpha+20])
	misquote := claimItem("a misquoted span", geo.alpha+6, geo.alpha+20, "words that are not there")
	crossChapter := claimItem("a span across chapters", geo.alpha+6, geo.beta+6, text[geo.alpha+6:geo.beta+6])

	status, body := postJSON(t, app.client, app.ts.URL+"/api/books/"+epubFixtureEntry+"/claims", map[string]any{
		"source": "test-extractor",
		"claims": []map[string]any{valid, misquote, crossChapter},
	})
	if status != http.StatusCreated {
		t.Fatalf("import status = %d: %v", status, body)
	}
	if body["stored"].(float64) != 1 || body["rejected"].(float64) != 2 {
		t.Fatalf("stored/rejected = %v/%v, want 1/2: %v", body["stored"], body["rejected"], body["results"])
	}
	results, _ := body["results"].([]any)
	reasons := ""
	for _, r := range results {
		res := r.(map[string]any)
		if res["status"] == "rejected" {
			reasons += res["error"].(string) + ";"
		}
		if res["status"] == "stored" && res["claim_id"] == "" {
			t.Error("a stored claim came back without its id")
		}
	}
	for _, want := range []string{"does not match the canonical text", "crosses a chapter boundary"} {
		if !strings.Contains(reasons, want) {
			t.Errorf("rejection reasons missing %q: %s", want, reasons)
		}
	}

	// The same import again is a no-op: identity is the evidence plus the
	// statement, and nothing duplicated.
	status, body = postJSON(t, app.client, app.ts.URL+"/api/books/"+epubFixtureEntry+"/claims", map[string]any{
		"source": "test-extractor", "claims": []map[string]any{valid},
	})
	if status != http.StatusCreated {
		t.Fatalf("re-import status = %d: %v", status, body)
	}
	_, claimsBody := app.get(t, "/api/books/"+epubFixtureEntry+"/claims?until=none")
	claims := claimsOf(t, claimsBody)
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want the one stored truth: %v", len(claims), claimsBody)
	}
	if claims[0]["quote"] != text[geo.alpha+6:geo.alpha+20] {
		t.Errorf("served quote = %v", claims[0]["quote"])
	}
	prov, _ := claims[0]["provenance"].(map[string]any)
	if prov == nil || prov["deep_link"] != deepLink(epubFixtureEntry, geo.alpha+6) {
		t.Errorf("claim provenance = %v", prov)
	}
}

// TestImportClaimsRequiresSource: an unattributed claim is rejected — a
// bad extractor run must be identifiable by what it wrote.
func TestImportClaimsRequiresSource(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	_, textBody := app.get(t, "/api/books/"+epubFixtureEntry+"/text?until=none")
	text := textBody["text"].(string)

	status, body := postJSON(t, app.client, app.ts.URL+"/api/books/"+epubFixtureEntry+"/claims", map[string]any{
		"claims": []map[string]any{
			claimItem("anonymous", geo.alpha, geo.alpha+5, text[geo.alpha:geo.alpha+5]),
		},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("sourceless import status = %d: %v", status, body)
	}
	results, _ := body["results"].([]any)
	if res := results[0].(map[string]any); !strings.Contains(res["error"].(string), "source") {
		t.Errorf("rejection = %v, want the missing source", res["error"])
	}
}

// TestClaimsClampToPosition: the leak bar over statements. Claims whose
// evidence sits past the position do not exist; a version revealed past
// the position does not exist — and neither does anything it says.
func TestClaimsClampToPosition(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	_, textBody := app.get(t, "/api/books/"+epubFixtureEntry+"/text?until=none")
	text := textBody["text"].(string)

	reveal := int(textBody["char_count"].(float64)) // replaced below with the real offset
	_ = reveal
	revealAt := strings.Index(text, leakReveal)
	if revealAt < 0 {
		t.Fatalf("fixture text lacks the reveal")
	}
	betaEnd := strings.Index(text[revealAt:], "\n")
	if betaEnd < 0 {
		betaEnd = len(text) - revealAt
	}

	// c1: read territory (Alpha), with a version whose evidence and
	// reveal are the confession paragraph in Beta.
	c1 := claimItem("the village is quiet", geo.alpha+6, geo.alpha+20, text[geo.alpha+6:geo.alpha+20])
	c1["subject"] = "the village"
	c1["versions"] = []map[string]any{{
		"statement":  leakReveal + " — the confession",
		"char_start": revealAt, "char_end": revealAt + 12,
		"quote":         text[revealAt : revealAt+12],
		"reveal_offset": revealAt,
	}}
	// c2: evidence inside Beta's innocent first paragraph.
	c2 := claimItem("the detective searched the manor", geo.beta+6, geo.candlestick, text[geo.beta+6:geo.candlestick])
	// c3: evidence is the reveal itself.
	c3 := claimItem("the culprit confessed", revealAt, revealAt+12, text[revealAt:revealAt+12])
	// c4: Gamma, the aftermath.
	c4 := claimItem("the trial changed the village", geo.gamma+6, geo.gamma+20, text[geo.gamma+6:geo.gamma+20])

	status, body := postJSON(t, app.client, app.ts.URL+"/api/books/"+epubFixtureEntry+"/claims", map[string]any{
		"source": "test-extractor",
		"claims": []map[string]any{c1, c2, c3, c4},
	})
	if status != http.StatusCreated {
		t.Fatalf("import status = %d: %v", status, body)
	}

	// Mid-Beta, before the reveal word: c1 (base) and c2 exist; the
	// version, the confession claim and the aftermath do not.
	app.setLeakPosition(t, geo.candlestick)
	resp, claimsBody := asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/claims", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token claims status = %d: %v", resp.StatusCode, claimsBody)
	}
	claims := claimsOf(t, claimsBody)
	if !hasStatement(claims, "the village is quiet") || !hasStatement(claims, "the detective searched the manor") {
		t.Errorf("read claims missing: %v", statementsOf(claims))
	}
	raw, _ := json.Marshal(claimsBody)
	assertNoLeak(t, "clamped claims", string(raw))
	for _, absent := range []string{"the culprit confessed", "the trial changed the village", leakReveal + " — the confession"} {
		if hasStatement(claims, absent) {
			t.Errorf("claim %q was served before its evidence was read", absent)
		}
	}

	// Read exactly through Alpha: Beta's innocent paragraph goes too.
	app.setLeakPosition(t, geo.beta)
	_, claimsBody = asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/claims", nil)
	claims = claimsOf(t, claimsBody)
	if hasStatement(claims, "the detective searched the manor") {
		t.Error("a claim whose evidence sits entirely past the bound was served")
	}
	if !hasStatement(claims, "the village is quiet") {
		t.Errorf("the read claim vanished: %v", statementsOf(claims))
	}

	// The cookie session asking out loud, and the token asking out loud,
	// both see everything — spoilers are an opt-in, not a lock.
	_, full := app.get(t, "/api/books/"+epubFixtureEntry+"/claims?until=none")
	claims = claimsOf(t, full)
	if !hasStatement(claims, "the culprit confessed") || !hasStatement(claims, "the trial changed the village") {
		t.Errorf("until=none withheld truths: %v", statementsOf(claims))
	}
}

// TestClaimVersionsServeByReveal: past the reveal, the version is the
// truth — nothing overwritten, the base still the base.
func TestClaimVersionsServeByReveal(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	_, textBody := app.get(t, "/api/books/"+epubFixtureEntry+"/text?until=none")
	text := textBody["text"].(string)
	revealAt := strings.Index(text, leakReveal)
	if revealAt < 0 {
		t.Fatalf("fixture text lacks the reveal")
	}

	c1 := claimItem("the village is quiet", geo.alpha+6, geo.alpha+20, text[geo.alpha+6:geo.alpha+20])
	c1["versions"] = []map[string]any{{
		"statement":  "the culprit was the butler",
		"char_start": revealAt, "char_end": revealAt + 12,
		"quote": text[revealAt : revealAt+12],
	}}
	status, body := postJSON(t, app.client, app.ts.URL+"/api/books/"+epubFixtureEntry+"/claims", map[string]any{
		"source": "test-extractor", "claims": []map[string]any{c1},
	})
	if status != http.StatusCreated {
		t.Fatalf("import status = %d: %v", status, body)
	}

	app.setLeakPosition(t, geo.gamma) // past the confession, into Gamma
	_, served := app.get(t, "/api/books/"+epubFixtureEntry+"/claims")
	claims := claimsOf(t, served)
	if len(claims) != 1 {
		t.Fatalf("claims = %d, want 1: %v", len(claims), served)
	}
	if claims[0]["statement"] != "the culprit was the butler" {
		t.Errorf("effective statement = %v", claims[0]["statement"])
	}
	if claims[0]["superseded"] != true || claims[0]["versions"].(float64) != 1 {
		t.Errorf("superseded/versions = %v/%v, want true/1", claims[0]["superseded"], claims[0]["versions"])
	}
	if int(claims[0]["reveal_offset"].(float64)) != revealAt {
		t.Errorf("reveal_offset = %v, want %d", claims[0]["reveal_offset"], revealAt)
	}
}

// TestClaimsStaleHiddenNotDeleted: a changed chapter hides its claims
// until an extractor re-anchors them — the row survives, the re-import
// heals it.
func TestClaimsStaleHiddenNotDeleted(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	_, textBody := app.get(t, "/api/books/"+epubFixtureEntry+"/text?until=none")
	text := textBody["text"].(string)

	item := claimItem("the village slept under the hill", geo.alpha+6, geo.alpha+20, text[geo.alpha+6:geo.alpha+20])
	status, body := postJSON(t, app.client, app.ts.URL+"/api/books/"+epubFixtureEntry+"/claims", map[string]any{
		"source": "test-extractor", "claims": []map[string]any{item},
	})
	if status != http.StatusCreated {
		t.Fatalf("import status = %d: %v", status, body)
	}

	// A re-ingest that changed Alpha's text: simulate the claim's anchor
	// against the old chapter (the hash is the key, so a stale one says
	// the same thing as a changed chapter).
	if _, err := app.store.DB().Exec(`UPDATE book_claims SET chapter_hash = 'stale'`); err != nil {
		t.Fatalf("stale the claim: %v", err)
	}
	_, served := app.get(t, "/api/books/"+epubFixtureEntry+"/claims?until=none")
	if claims := claimsOf(t, served); len(claims) != 0 {
		t.Errorf("a stale claim was served: %v", claims)
	}
	if served["stale_hidden"].(float64) != 1 {
		t.Errorf("stale_hidden = %v, want 1", served["stale_hidden"])
	}

	// The heal: re-importing against the current text re-anchors the row
	// (identity holds, the hash refreshes) and the truth returns.
	status, body = postJSON(t, app.client, app.ts.URL+"/api/books/"+epubFixtureEntry+"/claims", map[string]any{
		"source": "test-extractor", "claims": []map[string]any{item},
	})
	if status != http.StatusCreated {
		t.Fatalf("heal import status = %d: %v", status, body)
	}
	_, served = app.get(t, "/api/books/"+epubFixtureEntry+"/claims?until=none")
	if claims := claimsOf(t, served); len(claims) != 1 {
		t.Errorf("the healed claim did not return: %v", served)
	}
}

// TestClaimsTokenWriteScope: the read-only wall, and the one scope that
// names this POST.
func TestClaimsTokenWriteScope(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	_, textBody := app.get(t, "/api/books/"+epubFixtureEntry+"/text?until=none")
	text := textBody["text"].(string)
	item := claimItem("the village slept under the hill", geo.alpha+6, geo.alpha+20, text[geo.alpha+6:geo.alpha+20])
	payload := map[string]any{"source": "test-extractor", "claims": []map[string]any{item}}

	jar, _ := cookiejar.New(nil)
	writer := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	register(t, app.ts.URL, writer, "writer@example.com", "writer", "hogwash123")

	// A read-only token cannot import; the wall's message says so.
	readOnly := mintTokenScopes(t, app.ts.URL, writer, "books:read")
	resp, out := asLeakToken(t, app.ts.URL, readOnly, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/claims", payload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("read-only token import status = %d: %v", resp.StatusCode, out)
	}

	// The claims-scoped token can — against its own shelf. The fixture
	// entry belongs to the app's owner, so mint for the owner instead.
	writer2 := mintTokenScopes(t, app.ts.URL, app.client, "books:read", "claims:write")
	resp, out = asLeakToken(t, app.ts.URL, writer2, http.MethodPost,
		"/api/books/"+epubFixtureEntry+"/claims", payload)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("claims:write token import status = %d: %v", resp.StatusCode, out)
	}

	// Reads need no write scope: the leak fixture's token lists them.
	resp, out = asLeakToken(t, app.ts.URL, app.secret, http.MethodGet,
		"/api/books/"+epubFixtureEntry+"/claims?until=none", nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(claimsOf(t, out)) != 1 {
		t.Errorf("token claims read = %d, %v", resp.StatusCode, out)
	}
}

// TestClaimEntities: the cast the truths speak of, folded and aliased,
// with an entity first appearing past the position simply not existing.
func TestClaimEntities(t *testing.T) {
	app := newLeakApp(t)
	geo := leakGeometryOf(t, app)
	_, textBody := app.get(t, "/api/books/"+epubFixtureEntry+"/text?until=none")
	text := textBody["text"].(string)
	revealAt := strings.Index(text, leakReveal)
	if revealAt < 0 {
		t.Fatalf("fixture text lacks the reveal")
	}

	read := claimItem("eileen keeps the hill", geo.alpha+6, geo.alpha+20, text[geo.alpha+6:geo.alpha+20])
	read["subject"] = "Eileen"
	later := claimItem("the butler confesses by morning", revealAt, revealAt+12, text[revealAt:revealAt+12])
	later["subject"] = "The butler"

	status, body := postJSON(t, app.client, app.ts.URL+"/api/books/"+epubFixtureEntry+"/claims", map[string]any{
		"source": "test-extractor",
		"claims": []map[string]any{read, later},
		"entities": []map[string]any{
			{"name": "Eileen", "kind": "person", "aliases": []string{"Ellie"}},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("import status = %d: %v", status, body)
	}

	app.setLeakPosition(t, geo.candlestick)
	_, served := app.get(t, "/api/books/"+epubFixtureEntry+"/entities")
	raw, _ := json.Marshal(served)
	assertNoLeak(t, "clamped entities", string(raw))
	entities, _ := served["entities"].([]any)
	var eileen map[string]any
	names := []string{}
	for _, e := range entities {
		ent := e.(map[string]any)
		names = append(names, ent["name"].(string))
		if ent["name"] == "Eileen" {
			eileen = ent
		}
	}
	for _, banned := range []string{"The butler"} {
		for _, name := range names {
			if strings.EqualFold(name, banned) {
				t.Errorf("entity %q exists before its first mention was read", name)
			}
		}
	}
	if eileen == nil {
		t.Fatalf("Eileen missing from %v", names)
	}
	if eileen["claims"].(float64) != 1 {
		t.Errorf("Eileen claims = %v, want 1 (the butler claim is past the bound)", eileen["claims"])
	}
	if eileen["kind"] != "person" {
		t.Errorf("Eileen kind = %v", eileen["kind"])
	}
	first, _ := eileen["first_seen"].(map[string]any)
	if first == nil || int(first["char_start"].(float64)) != geo.alpha+6 {
		t.Errorf("Eileen first_seen = %v", first)
	}

	// The alias resolves; the entity filter narrows the claims.
	_, byAlias := app.get(t, "/api/books/"+epubFixtureEntry+"/entities?name=ellie")
	if entities, _ := byAlias["entities"].([]any); len(entities) != 1 {
		t.Errorf("alias lookup returned %d entities, want 1: %v", len(entities), byAlias)
	}
	_, filtered := app.get(t, "/api/books/"+epubFixtureEntry+"/claims?entity=ellie")
	if claims := claimsOf(t, filtered); !hasStatement(claims, "eileen keeps the hill") {
		t.Errorf("entity-filtered claims = %v", statementsOf(claims))
	}
}
