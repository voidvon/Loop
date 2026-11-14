package desktop

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func (p *benchmarkPane) chooseNodes() {
	if p.window == nil || p.probing || !p.available {
		return
	}
	p.Refresh()
	chosen := map[string]bool{}
	for key, value := range p.selected {
		chosen[key] = value
	}
	status := quietLabel("")
	update := func() {
		status.SetText(fmt.Sprintf("已选 %d / %d · 最多 128 个，逐个测试", len(chosen), len(p.nodes)))
	}
	var list *widget.List
	list = widget.NewList(func() int { return len(p.nodes) }, func() fyne.CanvasObject {
		check := widget.NewCheck("", nil)
		row := newListRow(nil)
		row.setLeading(check)
		return row
	}, func(id widget.ListItemID, object fyne.CanvasObject) {
		node := p.nodes[id]
		row := object.(*infoRow)
		row.setText(node.Name, node.Source)
		check := row.leadingObject.(*widget.Check)
		check.OnChanged = nil
		check.SetChecked(chosen[node.Key])
		check.OnChanged = func(value bool) {
			if value {
				if len(chosen) >= 128 && !chosen[node.Key] {
					check.SetChecked(false)
					status.SetText("一次最多选择 128 个节点")
					return
				}
				chosen[node.Key] = true
			} else {
				delete(chosen, node.Key)
			}
			update()
		}
	})
	list.HideSeparators = true
	all := widget.NewButton("全选（最多 128）", func() {
		for _, node := range p.nodes {
			if len(chosen) >= 128 {
				break
			}
			chosen[node.Key] = true
		}
		list.Refresh()
		update()
	})
	clear := widget.NewButton("清空", func() { chosen = map[string]bool{}; list.Refresh(); update() })
	update()
	content := container.NewBorder(container.NewBorder(nil, nil, container.NewHBox(all, clear), nil, status), nil, nil, nil, list)
	d := dialog.NewCustomConfirm("选择测速节点", "确定", "取消", content, func(ok bool) {
		if ok {
			p.selected = chosen
			p.updateControls()
		}
	}, p.window)
	d.Resize(fyne.NewSize(600, 440))
	d.Show()
}
