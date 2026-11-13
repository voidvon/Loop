package desktop

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

type systemFonts struct {
	regular, semibold, monospace, monoBold, symbols fyne.Resource
}

// Keep only the system file and chosen face. Fyne's font painter can read the
// selected tables directly, without retaining a second complete SFNT buffer.
type systemFontResource struct {
	name, path string
	index      int
}

func (r *systemFontResource) Name() string            { return r.name + ".otf" }
func (r *systemFontResource) FontFile() (string, int) { return r.path, r.index }
func (r *systemFontResource) SystemFontName() string  { return r.name }

func (r *systemFontResource) Content() []byte {
	file, err := os.Open(r.path)
	if err != nil {
		return nil
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil
	}
	data, _ := standaloneFontAt(file, info.Size(), r.index)
	return data
}

var _ fyne.FontFileResource = (*systemFontResource)(nil)
var _ fyne.SystemFontResource = (*systemFontResource)(nil)

// Native faces use the system's font registry. Content remains a portable
// fallback for consumers that do not implement the optional native renderer.
type namedSystemFont struct {
	fyne.Resource
	name string
}

func (r namedSystemFont) Name() string           { return r.name + ".ttf" }
func (r namedSystemFont) SystemFontName() string { return r.name }

// Read fonts from this Mac at runtime. Apple's font data is never bundled or
// persisted. Recent macOS versions install PingFang in a MobileAsset directory.
func loadSystemFonts() (*systemFonts, error) {
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	paths := []string{"/System/Library/Fonts/PingFang.ttc", "/System/Library/Fonts/LanguageSupport/PingFang.ttc"}
	assets, _ := filepath.Glob("/System/Library/AssetsV2/com_apple_MobileAsset_Font*/*.asset/AssetData/PingFang.ttc")
	paths = append(paths, assets...)
	for _, path := range paths {
		if fonts, err := loadSystemFontFile(path); err == nil {
			return fonts, nil
		}
	}
	return nil, errors.New("系统苹方字体不可用，使用 Fyne 默认字体")
}

func loadSystemFontFile(path string) (*systemFonts, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	// Inspect table directories and names only. Parsing every face just to find
	// two names allocates glyph data for unrelated weights and locales; reading
	// the entire TTC also creates a large temporary copy at startup.
	loaders, err := opentype.NewLoaders(file)
	if err != nil {
		return nil, err
	}
	regular, err := fontFaceResource(path, loaders, "PingFangSC-Regular")
	if err != nil {
		return nil, err
	}
	semibold, err := fontFaceResource(path, loaders, "PingFangSC-Semibold")
	if err != nil {
		return nil, err
	}
	return &systemFonts{
		regular: regular, semibold: semibold,
		monospace: namedSystemFont{theme.DefaultTextMonospaceFont(), "Menlo-Regular"},
		monoBold:  namedSystemFont{theme.DefaultTextMonospaceFont(), "Menlo-Bold"},
		symbols:   namedSystemFont{theme.DefaultSymbolFont(), "AppleSymbols"},
	}, nil
}

func fontFaceResource(path string, loaders []*opentype.Loader, postscriptName string) (fyne.Resource, error) {
	for i, loader := range loaders {
		raw, err := loader.RawTable(opentype.MustNewTag("name"))
		if err != nil {
			continue
		}
		names, _, err := tables.ParseName(raw)
		if err != nil || names.Name(tables.NameID(6)) != postscriptName {
			continue
		}
		return &systemFontResource{name: postscriptName, path: path, index: i}, nil
	}
	return nil, fmt.Errorf("字体中未找到 %s", postscriptName)
}

func standaloneFont(data []byte, index int) ([]byte, error) {
	return standaloneFontAt(bytes.NewReader(data), int64(len(data)), index)
}

// Content consumers expect standalone SFNT faces. Rebuild the chosen face's
// directory with relocated tables, preserving outlines and glyph metrics.
func standaloneFontAt(data io.ReaderAt, size int64, index int) ([]byte, error) {
	invalid := errors.New("无效的系统字体目录")
	if size < 12 || index < 0 {
		return nil, invalid
	}
	var header [12]byte
	if _, err := data.ReadAt(header[:], 0); err != nil {
		return nil, invalid
	}
	offset := uint64(0)
	if string(header[:4]) == "ttcf" {
		count := binary.BigEndian.Uint32(header[8:12])
		entry := uint64(12) + uint64(index)*4
		if uint64(index) >= uint64(count) || entry+4 > uint64(size) {
			return nil, invalid
		}
		var selected [4]byte
		if _, err := data.ReadAt(selected[:], int64(entry)); err != nil {
			return nil, invalid
		}
		offset = uint64(binary.BigEndian.Uint32(selected[:]))
	} else if index != 0 {
		return nil, invalid
	}
	if offset+12 > uint64(size) {
		return nil, invalid
	}
	if _, err := data.ReadAt(header[:], int64(offset)); err != nil {
		return nil, invalid
	}
	count := uint64(binary.BigEndian.Uint16(header[4:6]))
	directorySize := 12 + count*16
	if count == 0 || offset+directorySize > uint64(size) {
		return nil, invalid
	}
	directory := make([]byte, directorySize)
	if _, err := data.ReadAt(directory, int64(offset)); err != nil {
		return nil, invalid
	}
	total := directorySize
	for i := uint64(0); i < count; i++ {
		entry := 12 + i*16
		source := uint64(binary.BigEndian.Uint32(directory[entry+8 : entry+12]))
		length := uint64(binary.BigEndian.Uint32(directory[entry+12 : entry+16]))
		if source+length > uint64(size) {
			return nil, invalid
		}
		total += (length + 3) &^ 3
		if total > uint64(^uint32(0)) {
			return nil, invalid
		}
	}
	// Allocate once at the exact size, avoiding repeated copies and excess
	// capacity when appending the large CFF table from the system collection.
	result := make([]byte, total)
	copy(result, directory)
	headOffset, target := 0, int(directorySize)
	for i := uint64(0); i < count; i++ {
		entry := 12 + i*16
		source := int64(binary.BigEndian.Uint32(result[entry+8 : entry+12]))
		length := int(binary.BigEndian.Uint32(result[entry+12 : entry+16]))
		binary.BigEndian.PutUint32(result[entry+8:entry+12], uint32(target))
		if _, err := data.ReadAt(result[target:target+length], source); err != nil {
			return nil, invalid
		}
		if string(result[entry:entry+4]) == "head" {
			if length < 12 {
				return nil, invalid
			}
			headOffset = target
			// head's table checksum excludes checkSumAdjustment.
			clear(result[target+8 : target+12])
		}
		target += (length + 3) &^ 3
	}
	if headOffset == 0 {
		return nil, invalid
	}
	sum := uint32(0)
	for i := 0; i < len(result); i += 4 {
		sum += binary.BigEndian.Uint32(result[i : i+4])
	}
	binary.BigEndian.PutUint32(result[headOffset+8:headOffset+12], 0xB1B0AFBA-sum)
	return result, nil
}
