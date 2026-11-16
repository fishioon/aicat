package codex

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

// prompt is Codex's generic system prompt (Apache-2.0, openai/codex,
// core/gpt-5.2-codex_prompt.md), as Magpie (MIT, github.com/yetone/magpie)
// embeds it. A provider's model needs one: Codex's own catalog only carries
// prompts for OpenAI's models.
//
//go:embed prompt.md
var prompt string

// efforts are the reasoning levels Codex accepts; a catalog entry with any
// other would not load.
var efforts = []string{"none", "minimal", "low", "medium", "high", "xhigh"}

// entry is one model as Codex's models.json describes it. Only fields Codex
// requires or that change its behaviour are set. The shape follows Magpie's
// codexcat.Entries.
type entry struct {
	Slug          string  `json:"slug"`
	DisplayName   string  `json:"display_name"`
	Description   string  `json:"description"`
	Instructions  string  `json:"base_instructions"`
	DefaultEffort *string `json:"default_reasoning_level"`
	Efforts       []level `json:"supported_reasoning_levels"`
	Shell         string  `json:"shell_type"`
	Visibility    string  `json:"visibility"`
	InAPI         bool    `json:"supported_in_api"`
	Priority      int     `json:"priority"`
	Verbosity     bool    `json:"support_verbosity"`
	DefVerbosity  *string `json:"default_verbosity"`
	ApplyPatch    string  `json:"apply_patch_tool_type"`
	Truncation    struct {
		Mode  string `json:"mode"`
		Limit int    `json:"limit"`
	} `json:"truncation_policy"`
	Tools      []string `json:"experimental_supported_tools"`
	Modalities []string `json:"input_modalities"`
	Context    *int     `json:"context_window,omitempty"`
	Tiers      []string `json:"service_tiers"`
	// aicat serves no tool search of its own, unlike Magpie
	SearchTool bool `json:"supports_search_tool"`
	// required from Codex 0.147
	Parallel bool `json:"supports_parallel_tool_calls"`
}

type level struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

// entries renders the providers' models Codex may pick, ranked after the
// first `after`, filling what a provider's list leaves out from models.dev.
func (h *Handler) entries(after int) []entry {
	models := h.providers.ResponsesModels()
	type modelDetails struct {
		Name    string
		Context int
		Efforts []string
		Images  bool
	}
	details := make([]modelDetails, len(models))
	names := make([]string, len(models))
	supplemented := make([]bool, len(models))
	owners := map[string]string{}
	collisions := map[string]bool{}
	claim := func(name, id string) {
		if name == "" {
			return
		}
		if owner, ok := owners[name]; ok && owner != id {
			collisions[name] = true
		}
		owners[name] = id
	}
	for i, m := range models {
		if h.catalog != nil {
			if found, ok := h.catalog.Lookup(m.ID); ok {
				details[i] = modelDetails{Name: found.Name, Context: found.Context, Efforts: found.Efforts, Images: found.Images}
			}
		}
		name := m.Name
		if name == "" || name == m.ID {
			name = firstOf(details[i].Name, m.ID)
			supplemented[i] = true
		}
		names[i] = name
	}
	for i, m := range models {
		id := m.Provider + "/" + m.ID
		claim(id, id)
		claim(names[i], id)
	}
	for i, m := range models {
		if supplemented[i] && collisions[names[i]] {
			names[i] = m.ID
		}
	}
	out := make([]entry, 0, len(models))
	for i, m := range models {
		info := details[i]
		name := names[i]
		vendor := m.ProviderName
		e := entry{
			Slug: m.Provider + "/" + m.ID, DisplayName: name + " · " + vendor, Description: name + " (" + vendor + ") via aicat",
			Instructions: prompt, Efforts: []level{}, Shell: "unified_exec", Visibility: "list", InAPI: true,
			Priority: after + i + 1, ApplyPatch: "freeform", Tools: []string{}, Modalities: []string{"text"},
			Tiers: []string{}, Parallel: true,
		}
		e.Truncation.Mode, e.Truncation.Limit = "tokens", 10000
		images := info.Images
		if m.Images != nil {
			images = *m.Images // the vendor's own answer first
		}
		if images {
			e.Modalities = append(e.Modalities, "image")
		}
		if c := firstNonZero(m.Context, info.Context); c > 0 {
			e.Context = &c
		}
		levels := m.Efforts
		if len(levels) == 0 {
			levels = info.Efforts
		}
		for _, effort := range levels {
			if slices.Contains(efforts, effort) && !slices.ContainsFunc(e.Efforts, func(l level) bool { return l.Effort == effort }) {
				e.Efforts = append(e.Efforts, level{Effort: effort, Description: effort})
			}
		}
		if len(e.Efforts) > 0 {
			d := defaultEffort(e.Efforts)
			e.DefaultEffort = &d
		}
		out = append(out, e)
	}
	counts := make(map[string]int, len(out))
	for _, e := range out {
		counts[e.DisplayName]++
	}
	for i, e := range out {
		if counts[e.DisplayName] < 2 {
			continue
		}
		m := models[i]
		name := strings.TrimSuffix(e.DisplayName, " · "+m.ProviderName)
		model := m.Provider + "/" + m.ID
		out[i].DisplayName = name + " (" + model + ") · " + m.ProviderName
	}
	return out
}

// defaultEffort is the middle of the road: medium or high when offered, else
// the first.
func defaultEffort(levels []level) string {
	for _, want := range []string{"medium", "high"} {
		if slices.ContainsFunc(levels, func(l level) bool { return l.Effort == want }) {
			return want
		}
	}
	return levels[0].Effort
}

// Codex keeps the model list it was handed with the list's ETag, and asks
// again when a reply's X-Models-Etag differs from it. The ETag aicat hands on
// carries a tag of the providers' models too, so adding one, a sync, or a
// catalog update has Codex ask for the list again.

// tag names the providers' models as Codex is offered them.
func (h *Handler) tag() string {
	b, _ := json.Marshal(h.entries(0))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:6])
}

const tagMark = "+aicat-"

// withTag is an ETag (or none) with aicat's tag in it: W/"abc" becomes
// W/"abc+aicat-<tag>".
func withTag(etag, tag string) string {
	if strings.HasSuffix(etag, `"`) && len(etag) > 1 {
		return strings.TrimSuffix(etag, `"`) + tagMark + tag + `"`
	}
	return etag + tagMark + tag
}

func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}
