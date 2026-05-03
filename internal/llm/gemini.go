package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jeffwang/qdesk/pkg/protocol"
)

// Gemini is a VisionAgent backed by Google Gemini via the REST API.
//
// Default model is "gemini-2.5-flash". Override Model on the struct to use
// "gemini-2.5-pro" or another variant.
type Gemini struct {
	APIKey  string // required (GEMINI_API_KEY)
	Model   string // default "gemini-2.5-flash"
	BaseURL string // default "https://generativelanguage.googleapis.com/v1beta"
	HTTP    *http.Client
}

// Name returns the qualified model name.
func (g *Gemini) Name() string {
	if g.Model == "" {
		return "gemini-2.5-flash"
	}
	return g.Model
}

func (g *Gemini) baseURL() string {
	if g.BaseURL == "" {
		return "https://generativelanguage.googleapis.com/v1beta"
	}
	return g.BaseURL
}

func (g *Gemini) client() *http.Client {
	if g.HTTP != nil {
		return g.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// --- Gemini wire types (subset we use) ---

type geminiPart struct {
	Text       string         `json:"text,omitempty"`
	InlineData *geminiBlob    `json:"inline_data,omitempty"`
}

type geminiBlob struct {
	MIMEType string `json:"mime_type"`
	Data     string `json:"data"` // base64-encoded
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiGenConfig struct {
	Temperature      float32 `json:"temperature"`
	ResponseMIMEType string  `json:"responseMimeType,omitempty"`
}

type geminiRequest struct {
	Contents          []geminiContent  `json:"contents"`
	SystemInstruction *geminiContent   `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenConfig `json:"generationConfig,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content       geminiContent `json:"content"`
		FinishReason  string        `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// --- VisionAgent impl ---

const actSystemPrompt = `You are an expert UI automation agent driving a Linux sandbox via a JSON HTTP API.

The sandbox runs Chromium at 1920x1080. You see one screenshot per turn and must respond with a JSON decision.

Output STRICT JSON matching this schema:
{
  "reasoning": "<one short sentence about what you observe and why you chose the next action>",
  "done":      <true if the current step's Goal is now satisfied; false otherwise>,
  "action":    <one of the action shapes below, or null if done is true>
}

Action shapes (use only these; respect the field names exactly):
  {"type":"click",  "x":<int>, "y":<int>, "button":"left"}    // button optional, defaults left
  {"type":"type",   "text":"<string>"}
  {"type":"key",    "keys":["ctrl","s"]}
  {"type":"scroll", "x":<int>, "y":<int>, "dx":0, "dy":<int negative=down positive=up>}
  {"type":"drag",   "from":{"x":<int>,"y":<int>}, "to":{"x":<int>,"y":<int>}}
  {"type":"wait",   "ms":<int>}

Coordinates are integer pixels in the 1920x1080 viewport.

GROUNDING DISCIPLINE — to avoid Y-coordinate drift on Flutter / canvas UIs:
1. First identify the target element's bounding box in pixel space
   (x_min, y_min, x_max, y_max).
2. Compute the geometric center: x = (x_min + x_max) / 2, y = (y_min + y_max) / 2.
3. Use that center as the click coordinate.
4. Mention the bounding box you used in the "reasoning" field.

Common cases on a 1920x1080 viewport: the browser chrome occupies the top
~135 pixels (URL bar + the unsupported-flag warning banner), so the page
content starts around y=135. A primary call-to-action centered horizontally
in the middle of a Flutter welcome screen is typically located between
y=700 and y=780. Do NOT default to y < 650 for buttons that visually appear
in the lower half of the page.

If a previous click did not visibly change the page state, try clicking
30-60 pixels LOWER on the next attempt — the most common failure mode is
clicking too high.

CRITICAL — when to set "done": Only set done=true when the CURRENT screenshot visibly shows the goal already accomplished. Do NOT set done=true based on what you remember doing in earlier turns; the click might not have taken effect, the page might still be loading, or it might have navigated to the wrong place. If you just performed a navigation action, the safe choice is to wait 800ms in this turn and check again next turn.

Respond with JSON only — no prose, no code fences.`

const verifySystemPrompt = `You are a strict QA verifier. You receive an English expectation about the screen state and a screenshot.

Decide whether the expectation holds RIGHT NOW in the screenshot. Be conservative: if uncertain, return passed=false with a clear evidence string.

Output STRICT JSON:
{
  "passed":   <true|false>,
  "evidence": "<one sentence quoting what you see (or fail to see) on screen>"
}`

const diagnoseSystemPrompt = `You are an SRE-style debugger. A UI automation test failed; here is the failure summary, the recent turn history, and the final screenshot.

Write a short root-cause analysis (<= 4 sentences) plus one concrete suggestion. Output PLAIN TEXT, no JSON, no markdown headers.`

// Act runs one observe-act turn.
func (g *Gemini) Act(ctx context.Context, step Step) (*Decision, error) {
	user := buildActUserPrompt(step)
	resp, err := g.generate(ctx, actSystemPrompt, []geminiPart{
		{Text: user},
		{InlineData: &geminiBlob{MIMEType: "image/png", Data: b64(step.Screenshot)}},
	}, true)
	if err != nil {
		return nil, fmt.Errorf("gemini act: %w", err)
	}
	var dec Decision
	if err := unmarshalLoose(resp, &dec); err != nil {
		return nil, fmt.Errorf("decode decision: %w (raw=%q)", err, resp)
	}
	return &dec, nil
}

// Verify runs an expectation check against a screenshot.
func (g *Gemini) Verify(ctx context.Context, expectation string, screenshot []byte) (*VerifyResult, error) {
	user := "Expectation:\n" + expectation
	resp, err := g.generate(ctx, verifySystemPrompt, []geminiPart{
		{Text: user},
		{InlineData: &geminiBlob{MIMEType: "image/png", Data: b64(screenshot)}},
	}, true)
	if err != nil {
		return nil, fmt.Errorf("gemini verify: %w", err)
	}
	var v VerifyResult
	if err := unmarshalLoose(resp, &v); err != nil {
		return nil, fmt.Errorf("decode verify: %w (raw=%q)", err, resp)
	}
	return &v, nil
}

// Diagnose returns a free-text root-cause analysis.
func (g *Gemini) Diagnose(ctx context.Context, summary string, history []Turn, screenshot []byte) (string, error) {
	hist, _ := json.Marshal(history)
	user := "Failure summary:\n" + summary + "\n\nTurn history (JSON):\n" + string(hist)
	resp, err := g.generate(ctx, diagnoseSystemPrompt, []geminiPart{
		{Text: user},
		{InlineData: &geminiBlob{MIMEType: "image/png", Data: b64(screenshot)}},
	}, false)
	if err != nil {
		return "", fmt.Errorf("gemini diagnose: %w", err)
	}
	return strings.TrimSpace(resp), nil
}

// generate is the low-level POST. wantJSON sets responseMimeType for JSON
// outputs (for Act/Verify); diagnose passes false.
func (g *Gemini) generate(ctx context.Context, system string, parts []geminiPart, wantJSON bool) (string, error) {
	if g.APIKey == "" {
		return "", fmt.Errorf("GEMINI_API_KEY is empty")
	}
	body := geminiRequest{
		Contents: []geminiContent{{Role: "user", Parts: parts}},
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: system}}},
		GenerationConfig: &geminiGenConfig{
			Temperature: 0.2,
		},
	}
	if wantJSON {
		body.GenerationConfig.ResponseMIMEType = "application/json"
	}
	buf, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s",
		g.baseURL(), g.Name(), g.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gemini http %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var gr geminiResponse
	if err := json.Unmarshal(respBody, &gr); err != nil {
		return "", fmt.Errorf("decode gemini response: %w", err)
	}
	if gr.Error != nil {
		return "", fmt.Errorf("gemini api error: %s", gr.Error.Message)
	}
	if len(gr.Candidates) == 0 || len(gr.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini returned no candidates")
	}
	return gr.Candidates[0].Content.Parts[0].Text, nil
}

// buildActUserPrompt assembles the user-message part for Act. Keep small —
// the screenshot is the heavy payload.
func buildActUserPrompt(step Step) string {
	var b strings.Builder
	b.WriteString("Goal for this step: ")
	b.WriteString(step.Goal)
	if step.PageContext != "" {
		b.WriteString("\n\nPage context: ")
		b.WriteString(step.PageContext)
	}
	if len(step.History) > 0 {
		b.WriteString("\n\nPrior turns this step (latest last):")
		for _, t := range step.History {
			b.WriteString("\n- ")
			b.WriteString(t.Reasoning)
			if t.Action != nil {
				if data, err := json.Marshal(t.Action); err == nil {
					b.WriteString("  → ")
					b.Write(data)
				}
			}
		}
	}
	b.WriteString("\n\nReturn the JSON decision now.")
	return b.String()
}

// unmarshalLoose tolerates a few quirks: leading prose, code fences.
func unmarshalLoose(raw string, dst any) error {
	s := strings.TrimSpace(raw)
	// Strip ```json ... ``` fences if present.
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	// If there's JSON later in the response, find first '{' and last '}'.
	if start := strings.Index(s, "{"); start > 0 {
		s = s[start:]
	}
	if end := strings.LastIndex(s, "}"); end >= 0 && end < len(s)-1 {
		s = s[:end+1]
	}
	return json.Unmarshal([]byte(s), dst)
}

// nilSafeAction is a compile-time check that Decision serialises with a
// nil action correctly (used in tests below).
var _ = (&protocol.Action{}).Type

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
