package desktop

import (
	"encoding/binary"
	"runtime"
	"testing"

	"fyne.io/fyne/v2"
	"golang.org/x/image/font/sfnt"
)

func TestSystemFontFacesHaveCorrectNamesWeightsAndChineseGlyphs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS system fonts")
	}
	fonts, err := loadSystemFonts()
	if err != nil {
		t.Fatal(err)
	}
	for name, resource := range map[string]fyne.Resource{"PingFangSC-Regular": fonts.regular, "PingFangSC-Semibold": fonts.semibold} {
		data := resource.Content()
		if cap(data) != len(data) {
			t.Fatal("standalone font retains excess extraction capacity")
		}
		face, err := sfnt.Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		got, err := face.Name(nil, sfnt.NameIDPostScript)
		if err != nil || got != name {
			t.Fatalf("name = %q, error = %v", got, err)
		}
		for _, r := range "首页节点策略规则配置" {
			glyph, err := face.GlyphIndex(nil, r)
			if err != nil || glyph == 0 {
				t.Fatalf("%s lacks %c: %v", name, r, err)
			}
		}
		sum := uint32(0)
		for i := 0; i < len(data); i += 4 {
			sum += binary.BigEndian.Uint32(data[i : i+4])
		}
		if sum != 0xB1B0AFBA {
			t.Fatal("standalone face checksum is invalid")
		}
	}
	th := newClientTheme()
	th.fonts = fonts
	if th.Font(fyne.TextStyle{}) == th.Font(fyne.TextStyle{Bold: true}) {
		t.Fatal("bold uses the regular face")
	}
	if th.Font(fyne.TextStyle{Monospace: true}) == fonts.regular {
		t.Fatal("editor lost its monospace face")
	}
}

func TestStandaloneFontRejectsInvalidCollections(t *testing.T) {
	for _, data := range [][]byte{nil, []byte("ttcf"), make([]byte, 12), append([]byte("ttcf"), make([]byte, 20)...)} {
		if _, err := standaloneFont(data, 0); err == nil {
			t.Fatal("accepted an invalid font directory")
		}
	}
}
