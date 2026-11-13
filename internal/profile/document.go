package profile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

const MaxDocumentBytes = 2 << 20

type DocumentError struct {
	Line, Column int
	Message      string
}

func (e *DocumentError) Error() string {
	return fmt.Sprintf("第 %d 行，第 %d 列：%s", e.Line, e.Column, e.Message)
}

func documentError(data []byte, offset int64, message string) error {
	position := int(offset)
	if position < 0 {
		position = 0
	}
	if position > len(data) {
		position = len(data)
	}
	prefix := data[:position]
	line := bytes.Count(prefix, []byte{'\n'}) + 1
	last := bytes.LastIndexByte(prefix, '\n') + 1
	return &DocumentError{Line: line, Column: utf8.RuneCount(prefix[last:]) + 1, Message: message}
}

// ParseDocument rejects ambiguous duplicate keys and preserves numeric values
// exactly. Formatting must never silently round a large JSON integer.
func ParseDocument(data []byte) (map[string]any, error) {
	if len(data) > MaxDocumentBytes {
		return nil, errors.New("配置不能超过 2 MiB")
	}
	if !utf8.Valid(data) {
		return nil, errors.New("配置必须是 UTF-8 文本")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value func(int) (any, error)
	value = func(depth int) (any, error) {
		if depth > 256 {
			return nil, documentError(data, decoder.InputOffset(), "JSON 嵌套层数过多")
		}
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch token {
		case json.Delim('{'):
			object := map[string]any{}
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("对象键必须是字符串")
				}
				if _, exists := object[key]; exists {
					return nil, documentError(data, decoder.InputOffset(), "存在重复的 JSON 字段："+key)
				}
				child, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				object[key] = child
			}
			_, err := decoder.Token()
			return object, err
		case json.Delim('['):
			array := []any{}
			for decoder.More() {
				child, err := value(depth + 1)
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			_, err := decoder.Token()
			return array, err
		default:
			return token, nil
		}
	}
	parsed, err := value(0)
	if err != nil {
		var located *DocumentError
		if errors.As(err, &located) {
			return nil, err
		}
		var syntax *json.SyntaxError
		if errors.As(err, &syntax) {
			return nil, documentError(data, syntax.Offset-1, syntax.Error())
		}
		return nil, documentError(data, int64(len(data)), "JSON 不完整，请检查括号、逗号和引号")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, documentError(data, decoder.InputOffset(), "JSON 后有多余内容")
	}
	object, ok := parsed.(map[string]any)
	if !ok {
		return nil, documentError(data, 0, "配置必须是 JSON 对象")
	}
	return object, nil
}

func FormatDocument(data []byte) ([]byte, error) {
	object, err := ParseDocument(data)
	if err != nil {
		return nil, err
	}
	formatted, err := json.MarshalIndent(object, "", "  ")
	return append(formatted, '\n'), err
}

// CompileDocument validates the main source using an unstarted placeholder
// node, so global/direct mode or a missing selection cannot hide policy errors.
func (s Store) CompileDocument(name string, draft []byte) (map[string]any, error) {
	if name != MainConfigName {
		return nil, errors.New("请编辑主配置 config.json；节点与订阅在节点页管理")
	}
	object, err := ParseDocument(draft)
	if err != nil {
		return nil, err
	}
	inbounds, ok := object["inbounds"].([]any)
	if !ok || len(inbounds) != 1 {
		return nil, errors.New("主配置需要一个本地 mixed 入站；TUN 请在首页切换")
	}
	candidate := Store{Dir: s.Dir, documents: map[string][]byte{name: draft}}
	return candidate.compileNode(&Node{ID: "config-check", Name: "配置校验", Outbound: map[string]any{
		"type": "socks", "server": "127.0.0.1", "server_port": 9,
	}})
}

// SaveDocument validates before writing, detects external edits, and keeps one
// private backup. A rejected draft cannot replace a working configuration.
func SaveDocument(path string, draft, expected []byte, validate func([]byte) error) ([]byte, error) {
	if validate == nil {
		return nil, errors.New("保存前必须完成内核校验")
	}
	formatted, err := FormatDocument(draft)
	if err != nil {
		return nil, err
	}
	if err := validate(formatted); err != nil {
		return nil, err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(current, expected) {
		return nil, errors.New("配置文件已被其他操作修改，请先重新载入，再合并你的修改")
	}
	if err := writePrivateFile(path+".bak", current); err != nil {
		return nil, fmt.Errorf("备份原配置失败：%w", err)
	}
	if err := writePrivateFile(path, formatted); err != nil {
		return nil, err
	}
	return formatted, nil
}

// RestoreDocument rolls back a failed application without replacing the backup.
// Never overwrite edits made externally after the candidate was saved.
func RestoreDocument(path string, previous, expected []byte) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, expected) {
		return errors.New("配置文件已被其他操作修改，无法自动恢复原配置，请检查配置页")
	}
	return writePrivateFile(path, previous)
}
