//go:build !nogui

package app

import "github.com/egoist/mygo/ui"

// 与 ChatGPT 的系统字体栈及常用字号对应；DIP 不乘 Retina 缩放比例。
// Core Text 为英文选择系统 UI 字体，为中文选择系统的 CJK 回退字体。
const (
	fontUI              = "system-ui"
	fontCode            = "ui-monospace"
	textCaption float32 = 11
	textUI      float32 = 13
	textBody    float32 = 16
	textTitle   float32 = 20
	textCode    float32 = 12
	iconSize    float32 = 17
	iconTarget  float32 = 29
)

type palette struct{ bg, surface, border, text, muted, brand, brandText, brandSoft, success, danger, stripe, soft, info, warning ui.Color }

func colors(c *ui.Context) palette {
	p := palette{bg: ui.Hex("#f6f6f7"), surface: ui.Hex("#ffffff"), border: ui.Hex("#e1e1e5"), text: ui.Hex("#242428"), muted: ui.Hex("#707079"), brand: ui.Hex("#00bba7"), brandText: ui.Hex("#087e73"), brandSoft: ui.Hex("#e5f5f1"), success: ui.Hex("#18794e"), danger: ui.Hex("#b42318"), stripe: ui.Hex("#f6f7f7"), soft: ui.Hex("#ededf0"), info: ui.Hex("#375bb5"), warning: ui.Hex("#926117")}
	if c.Theme().Background.R < 100 {
		p.bg = ui.Hex("#252527")
		p.surface = ui.Hex("#202022")
		p.border = ui.Hex("#37373b")
		p.text = ui.Hex("#e9e9ed")
		p.muted = ui.Hex("#a6a6b0")
		// Keep dark accents on netcat's website palette: the brand color for
		// text, with a deeper teal surface instead of a pale, gray-green fill.
		p.brandText = p.brand
		p.brandSoft = ui.Hex("#123b38")
		p.success = ui.Hex("#79cba5")
		p.danger = ui.Hex("#ff8a7a")
		p.stripe = ui.Hex("#272729")
		p.soft = ui.Hex("#303033")
		p.info = ui.Hex("#9eb9ef")
		p.warning = ui.Hex("#f2bd5c")
	}
	t := *c.Theme()
	t.Font = fontUI
	t.FontSize = textUI
	t.Background = p.surface
	t.Surface = p.bg
	t.SurfaceHover = p.soft
	t.SurfacePressed = p.soft
	t.Border = p.border
	t.Text = p.text
	t.TextMuted = p.muted
	t.Accent = p.brand
	t.AccentText = ui.Hex("#111827")
	t.AccentHover = p.brand.Mix(p.surface, 0.1)
	t.AccentPressed = p.brandText
	t.Selection = p.brandSoft
	t.Focus = p.brand.Alpha(0.22)
	t.FocusRingWidth = 1
	t.FocusRingOffset = 0
	c.SetTheme(&t)
	return p
}

var icons = map[string]*ui.SVG{}

func icon(name string) *ui.SVG {
	if s := icons[name]; s != nil {
		return s
	}
	paths := map[string]string{
		"activity":   `<path d="M22 12h-4l-3 9-6-18-3 9H2"/>`,
		"extensions": `<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/><path d="m17 2 5 5-5 5-5-5Z"/>`,
		"close":      `<path d="m6 6 12 12M6 18 18 6"/>`,
		"more":       `<circle cx="5" cy="12" r="1.65" fill="currentColor" stroke="none"/><circle cx="12" cy="12" r="1.65" fill="currentColor" stroke="none"/><circle cx="19" cy="12" r="1.65" fill="currentColor" stroke="none"/>`,
		"back":       `<path d="m12 19-7-7 7-7m-7 7h14"/>`, "forward": `<path d="m12 5 7 7-7 7m7-7H5"/>`,
		"sidebar":       `<rect x="3" y="4" width="18" height="16" rx="3"/><path d="M9 4v16"/>`,
		"search":        `<circle cx="11" cy="11" r="7"/><path d="m16 16 4 4"/>`,
		"globe":         `<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a18 18 0 0 0 0 18 18 18 0 0 0 0-18"/>`,
		"folder-tree":   `<path d="M20 10h-7a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h2l2 2h3a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2ZM20 22h-7a2 2 0 0 1-2-2v-4a2 2 0 0 1 2-2h2l2 2h3a2 2 0 0 1 2 2v2a2 2 0 0 1-2 2ZM2 2v18a2 2 0 0 0 2 2h3M2 10h5"/>`,
		"monitor":       `<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8m-4-4v4"/>`,
		"chevron-right": `<path d="m9 18 6-6-6-6"/>`,
		"chevron-down":  `<path d="m6 9 6 6 6-6"/>`,
		"trash":         `<path d="M3 6h18M9 6V4h6v2M5 6l1 14h12l1-14M10 10v6m4-6v6"/>`,
		"pin":           `<path d="m15 3 6 6-4 1-4 4v3l-6-6h3l4-4 1-4ZM3 21l6-6"/>`,
		"download":      `<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>`,
		"sun":           `<circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1 1m12 12 1 1M5 19l1-1M18 6l1-1"/>`,
		"copy":          `<rect x="8" y="8" width="12" height="13" rx="2.5"/><path d="M16 8V5a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h2"/>`,
		"check":         `<path d="m20 6-11 11-5-5"/>`,
		"circle-alert":  `<circle cx="12" cy="12" r="10"/><path d="M12 8v4m0 4h.01"/>`,
		"refresh":       `<path d="M20 4v6h-6m6 0a8 8 0 1 0-1 8"/>`,
		"lock":          `<rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4"/>`,
	}
	s := ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">` + paths[name] + `</svg>`))
	icons[name] = s
	return s
}
func iconButton(c *ui.Context, key, name, label string) ui.Element {
	return ui.Button(c.Key(key), "").Label(tr(label)).Tooltip(tr(label)).Size(iconTarget, iconTarget).Shrink(0).Padding(6).Radius(8).Background(ui.Transparent).Border(0, ui.Transparent).Children(func() { ui.Icon(c, icon(name)).Size(iconSize, iconSize) })
}
