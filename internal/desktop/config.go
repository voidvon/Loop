package desktop

import (
	"bytes"
	"errors"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	prettyview "github.com/ideaconnect/go-fyne-pretty-view/v2"

	"local/loop/internal/profile"
)

type configDocument struct {
	name, path        string
	view              *prettyview.PrettyView
	baseline, initial []byte
	loaded            bool
	loadError         error
}

func (d *configDocument) dirty() bool {
	return d.loaded && !bytes.Equal(d.view.Source(), d.initial)
}

type configPane struct {
	root                        fyne.CanvasObject
	window                      fyne.Window
	controller                  *documentController
	status                      *widget.Label
	save, check, format, reload *widget.Button
	document                    *configDocument
}

func newConfigPane(window fyne.Window, controller *documentController) *configPane {
	p := &configPane{window: window, controller: controller, root: container.NewStack()}
	p.document = &configDocument{name: controller.name, path: controller.path}
	controller.HasDraft = p.HasDraft
	controller.OnSaved = func(saved []byte) {
		doc := p.document
		// Rule forms can save before the first editor visit. Read the current
		// file on first open instead of constructing an invisible editor.
		if doc.view == nil {
			return
		}
		doc.baseline = append([]byte(nil), saved...)
		doc.view.SetData(saved, prettyview.FormatJSON)
		doc.initial = append([]byte(nil), doc.view.Source()...)
	}
	return p
}

// Open allocates the editor and its renderers only on the first visit. Keep it
// thereafter so navigation preserves drafts, undo history and cursor position.
func (p *configPane) Open() {
	if p.document.view == nil {
		p.buildEditor()
	}
	p.Refresh()
}

func (p *configPane) buildEditor() {
	window := p.window
	p.status = quietLabel("")
	doc := p.document
	doc.view = prettyview.New(prettyview.WithEditable(), prettyview.WithLineNumbers(), prettyview.WithFormat(prettyview.FormatJSON), prettyview.WithInputConfig(prettyview.InputConfig{DebounceFor: 250 * time.Millisecond, AutoFormat: prettyview.AutoFormatOff, MaxEditBytes: profile.MaxDocumentBytes}))
	p.updateEditorPalette()
	doc.view.SetOnChanged(func(string) {
		if !p.controller.closed {
			if p.controller.OnStateChanged != nil {
				p.controller.changed()
			} else {
				p.updateStatus()
			}
		}
	})
	search := prettyview.NewSearchBar(doc.view)
	tools := container.NewHBox(widget.NewButton("展开", doc.view.ExpandAll), widget.NewButton("折叠", doc.view.CollapseAll))
	editor := container.NewBorder(container.NewBorder(nil, nil, tools, nil, search), nil, nil, nil, doc.view)
	p.format = widget.NewButton("格式化", func() {
		if _, err := profile.ParseDocument(p.document.view.Source()); err != nil {
			p.showError(err)
			return
		}
		p.document.view.Reformat()
		p.updateStatus()
	})
	p.check = widget.NewButton("校验", func() { p.run(false) })
	p.save = widget.NewButtonWithIcon("保存", theme.DocumentSaveIcon(), func() { p.run(true) })
	p.save.Importance = widget.HighImportance
	p.reload = widget.NewButtonWithIcon("重新载入", theme.ViewRefreshIcon(), func() {
		load := func() { p.document.loaded = false; p.load(); p.updateStatus() }
		if p.document.dirty() {
			dialog.ShowConfirm("放弃修改", "重新载入会放弃当前文档未保存的修改。", func(ok bool) {
				if ok {
					load()
				}
			}, window)
		} else {
			load()
		}
	})
	header := container.NewBorder(nil, nil, nil,
		container.NewCenter(container.NewHBox(p.reload, p.format, p.check, p.save)), pageHeading("配置"))
	p.root.(*fyne.Container).Add(container.NewBorder(inset(0, 12, 0, 0, header), inset(10, 0, 0, 0, p.status), nil, nil, panel(editor)))
}

func (p *configPane) load() {
	doc := p.document
	data, err := os.ReadFile(doc.path)
	doc.loadError = err
	if err != nil {
		return
	}
	if len(data) > profile.MaxDocumentBytes {
		doc.loadError = errors.New("配置不能超过 2 MiB")
		return
	}
	if doc.loaded && bytes.Equal(data, doc.baseline) {
		return
	}
	doc.baseline = append([]byte(nil), data...)
	if formatted, formatErr := profile.FormatDocument(data); formatErr == nil {
		data = formatted
	}
	doc.view.SetData(data, prettyview.FormatJSON)
	doc.initial = append([]byte(nil), doc.view.Source()...)
	doc.loaded = true
}

func (p *configPane) Refresh() {
	if p.document == nil || p.document.view == nil {
		return
	}
	// A page refresh or node/mode switch must never replace an unsaved draft.
	if !p.document.dirty() && !p.controller.working {
		p.load()
	}
	p.updateStatus()
}

func (p *configPane) updateStatus() {
	if p.document == nil || p.document.view == nil {
		return
	}
	doc := p.document
	source := doc.view.Source()
	dirty := doc.loaded && !bytes.Equal(source, doc.initial)
	editable := doc.loadError == nil
	setEnabled(p.format, editable && !p.controller.working)
	setEnabled(p.check, editable && p.controller.available && !p.controller.working)
	setEnabled(p.save, editable && p.controller.available && !p.controller.working && dirty)
	setEnabled(p.reload, !p.controller.working)
	if doc.loadError != nil {
		p.status.SetText("读取失败：" + doc.loadError.Error())
		return
	}
	state := "已保存"
	if dirty {
		state = "未保存"
	}
	if _, err := profile.ParseDocument(source); err != nil {
		p.status.SetText(state + " · " + err.Error())
		return
	}
	if !p.controller.available {
		p.status.SetText(state + " · 可编辑草稿，请停止连接并等待其他操作完成后保存。")
		return
	}
	p.status.SetText(state + " · JSON 语法正确 · 保存时进行内核校验，原文件备份为 .bak")
}

func (p *configPane) SetAvailable(available bool) {
	p.controller.SetAvailable(available)
	p.updateStatus()
}

func (p *configPane) showError(err error) {
	p.status.SetText(err.Error())
	var position *profile.DocumentError
	if errors.As(err, &position) {
		p.document.view.SetCaret(position.Line-1, position.Column-1)
		p.window.Canvas().Focus(p.document.view)
	}
}

func (p *configPane) run(save bool) {
	if p.document == nil || p.document.loadError != nil {
		return
	}
	if _, err := profile.ParseDocument(p.document.view.Source()); err != nil {
		p.showError(err)
		return
	}
	p.controller.Run(p.document.view.Source(), p.document.baseline, save, func(err error) {
		p.updateStatus()
		if err != nil {
			p.showError(err)
		} else if save {
			p.status.SetText("保存成功 · 下次启动生效 · 原配置已备份为 .bak")
		} else {
			p.status.SetText("校验通过 · JSON 与 sing-box 配置有效；尚未保存。")
		}
	})
}

func (p *configPane) HasDraft() bool {
	return p.document != nil && p.document.dirty()
}

func (p *configPane) Close() { p.controller.Close() }

// PrettyView keys its syntax palette by OS variant. Reapply both variants when
// the application appearance changes so explicit modes also theme the editor.
func (p *configPane) updateEditorPalette() {
	if p.document == nil || p.document.view == nil {
		return
	}
	th := fyne.CurrentApp().Settings().Theme()
	for _, variant := range []fyne.ThemeVariant{theme.VariantLight, theme.VariantDark} {
		p.document.view.SetTheme(variant, prettyview.Theme{
			Key:    th.Color(colorAccent, variant),
			String: th.Color(theme.ColorNameSuccess, variant),
			Number: th.Color(colorOrange, variant), Bool: th.Color(colorOrange, variant),
			Null: th.Color(colorSecondary, variant), Punct: th.Color(colorSecondary, variant),
			Foreground: th.Color(theme.ColorNameForeground, variant), Summary: th.Color(colorSecondary, variant),
			IndentGuide: th.Color(theme.ColorNameSeparator, variant), Selection: th.Color(theme.ColorNameSelection, variant),
		})
	}
}
