package desktop

import (
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

var iconReject = outlineIcon("reject", `<path d="m12 2 8 3v6c0 5-4 8-8 11-4-3-8-6-8-11V5zM9 9l6 6m0-6-6 6"/>`)

func strategyGraphic(name, content string) fyne.Resource {
	return fyne.NewStaticResource("loop-strategy-"+name+".svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="28" height="24" viewBox="0 0 28 24">`+content+`</svg>`))
}

var strategyGraphics = map[string]fyne.Resource{
	"available": strategyGraphic("available", `<path d="M2 5h20v3H2zM2 11h20v3H2zM2 17h20v3H2z" fill="#95d600"/><path d="M24 5h3v3h-3zM24 11h3v3h-3zM24 17h3v3h-3z" fill="#38c921"/>`),
	"youtube":   strategyGraphic("youtube", `<rect x="0" y="3" width="28" height="18" rx="5" fill="#f52428"/><path d="m11 7 8 5-8 5z" fill="#fff"/>`),
	"telegram":  strategyGraphic("telegram", `<path d="M1 11 27 2l-5 21-8-7-4 4 1-7 11-7-13 5-2 5z" fill="#42b8e6"/><path d="m11 13 11-7-8 10-4 4z" fill="#8bd4ee"/>`),
	"hk":        strategyGraphic("hk", `<rect x="0" y="3" width="28" height="18" rx="3" fill="#e51c23"/><g fill="#fff"><ellipse cx="14" cy="8" rx="2" ry="3"/><ellipse cx="18" cy="11" rx="3" ry="2"/><ellipse cx="16" cy="15" rx="2" ry="3"/><ellipse cx="11" cy="15" rx="3" ry="2"/><ellipse cx="10" cy="10" rx="2" ry="3"/></g>`),
	"tw":        strategyGraphic("tw", `<rect x="0" y="3" width="28" height="18" rx="3" fill="#ef272e"/><path d="M0 3h14v10H0z" fill="#1456c0"/><circle cx="7" cy="8" r="3" fill="#fff"/>`),
	"sg":        strategyGraphic("sg", `<rect x="0" y="3" width="28" height="18" rx="3" fill="#fff"/><path d="M0 6a3 3 0 0 1 3-3h22a3 3 0 0 1 3 3v6H0z" fill="#eb303b"/><circle cx="7" cy="7.5" r="3.5" fill="#fff"/><circle cx="8.5" cy="7" r="3" fill="#eb303b"/><g fill="#fff"><circle cx="12" cy="5" r=".7"/><circle cx="14" cy="6.5" r=".7"/><circle cx="13.2" cy="9" r=".7"/><circle cx="10.8" cy="9" r=".7"/><circle cx="10" cy="6.5" r=".7"/></g>`),
	"jp":        strategyGraphic("jp", `<rect x="0" y="3" width="28" height="18" rx="3" fill="#fff"/><circle cx="14" cy="12" r="5" fill="#e22c36"/>`),
	"kr":        strategyGraphic("kr", `<rect x="0" y="3" width="28" height="18" rx="3" fill="#fff"/><circle cx="14" cy="12" r="5" fill="#e83d4d"/><path d="M9 12a5 5 0 0 0 10 0c0-3-5-3-5 0s-5 3-5 0" fill="#2863b5"/><g stroke="#333" stroke-width="1.2"><path d="m4 8 3-3m-2 5 3-3m-2 5 3-3M20 5l3 3m-4-1 3 3m-4-1 3 3M4 16l3 3m-2-5 3 3m-2-5 3 3m11 4 3-3m-4 1 3-3m-4 1 3-3"/></g>`),
	"us":        strategyGraphic("us", `<rect x="0" y="3" width="28" height="18" rx="3" fill="#fff"/><path d="M1 4h26M0 7h28M0 10h28M0 13h28M0 16h28M1 19h26" stroke="#e34251" stroke-width="1.5"/><path d="M0 3h12v10H0z" fill="#2363ae"/><path d="M2 5h1m2 0h1m2 0h1M3 7h1m2 0h1m2 0h1M2 9h1m2 0h1m2 0h1M3 11h1m2 0h1m2 0h1" stroke="#fff" stroke-width="1.2"/>`),
}

func strategyGroupIcon(name string) fyne.Resource {
	key := strings.ToLower(strings.TrimSpace(name))
	if icon, ok := strategyGraphics[key]; ok {
		return icon
	}
	for _, item := range []struct{ prefix, key string }{{"香港", "hk"}, {"台湾", "tw"}, {"新加坡", "sg"}, {"日本", "jp"}, {"韩国", "kr"}, {"美国", "us"}} {
		if strings.HasPrefix(key, item.prefix) {
			return strategyGraphics[item.key]
		}
	}
	return theme.NewColoredResource(iconPolicy, colorAccent)
}
