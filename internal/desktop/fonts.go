package desktop

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"fyne.io/fyne/v2"
	"golang.org/x/image/font/sfnt"
)

type systemFonts struct{ regular, semibold fyne.Resource }

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
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		regular, err := fontFaceResource(data, "PingFangSC-Regular")
		if err != nil {
			continue
		}
		semibold, err := fontFaceResource(data, "PingFangSC-Semibold")
		if err != nil {
			continue
		}
		return &systemFonts{regular: regular, semibold: semibold}, nil
	}
	return nil, errors.New("系统苹方字体不可用，使用 Fyne 默认字体")
}

func fontFaceResource(data []byte, postscriptName string) (fyne.Resource, error) {
	collection, err := sfnt.ParseCollection(data)
	if err != nil {
		return nil, err
	}
	for i := 0; i < collection.NumFonts(); i++ {
		face, err := collection.Font(i)
		if err != nil {
			continue
		}
		name, err := face.Name(nil, sfnt.NameIDPostScript)
		if err != nil || name != postscriptName {
			continue
		}
		standalone, err := standaloneFont(data, i)
		if err != nil {
			return nil, err
		}
		return fyne.NewStaticResource(postscriptName+".otf", standalone), nil
	}
	return nil, fmt.Errorf("字体中未找到 %s", postscriptName)
}

// Fyne accepts individual SFNT faces, not TTC collections. Rebuild the chosen
// face's directory with relocated tables, preserving outlines and glyph metrics.
func standaloneFont(data []byte, index int) ([]byte, error) {
	invalid := errors.New("无效的系统字体目录")
	if len(data) < 12 || index < 0 {
		return nil, invalid
	}
	offset := uint64(0)
	if string(data[:4]) == "ttcf" {
		count := binary.BigEndian.Uint32(data[8:12])
		entry := uint64(12) + uint64(index)*4
		if uint64(index) >= uint64(count) || entry+4 > uint64(len(data)) {
			return nil, invalid
		}
		offset = uint64(binary.BigEndian.Uint32(data[entry : entry+4]))
	} else if index != 0 {
		return nil, invalid
	}
	if offset+12 > uint64(len(data)) {
		return nil, invalid
	}
	count := uint64(binary.BigEndian.Uint16(data[offset+4 : offset+6]))
	end := offset + 12 + count*16
	if count == 0 || end > uint64(len(data)) {
		return nil, invalid
	}
	result := append([]byte(nil), data[offset:end]...)
	headOffset := 0
	for i := uint64(0); i < count; i++ {
		entry := 12 + i*16
		source := uint64(binary.BigEndian.Uint32(result[entry+8 : entry+12]))
		length := uint64(binary.BigEndian.Uint32(result[entry+12 : entry+16]))
		if source+length > uint64(len(data)) {
			return nil, invalid
		}
		target := len(result)
		binary.BigEndian.PutUint32(result[entry+8:entry+12], uint32(target))
		result = append(result, data[source:source+length]...)
		for len(result)%4 != 0 {
			result = append(result, 0)
		}
		if string(result[entry:entry+4]) == "head" {
			if length < 12 {
				return nil, invalid
			}
			headOffset = target
			// head's table checksum excludes checkSumAdjustment.
			clear(result[target+8 : target+12])
		}
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
