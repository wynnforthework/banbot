package utils

import (
	"os"
	"regexp"
	"strings"
)

var (
	commentRe   = regexp.MustCompile(`(\s+|\b)#[^\n]*`)
	emptyLineRe = regexp.MustCompile(`\n(\s*\n)+`)
	keyStartRe  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]`)
	commaRe     = regexp.MustCompile(`:($|\s)`)
)

// MergeYamlStr 合并多个yaml文件内容，保持键的顺序
func MergeYamlStr(paths []string, skips map[string]bool) (string, error) {
	keys := make([]string, 0)
	resMap := make(map[string]string)
	lastIdx := -1

	for _, path := range paths {
		// 读取文件内容
		content, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}

		// 删除注释和多余空行
		text := strings.ReplaceAll(string(content), "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
		text = commentRe.ReplaceAllString(text, "")
		text = emptyLineRe.ReplaceAllString(text, "\n")

		lines := strings.Split(text, "\n")
		var currentKey string
		var valueBuilder strings.Builder

		for i := 0; i < len(lines); i++ {
			line := lines[i]
			if line == "" {
				continue
			}

			// 检查是否是新的键开始
			if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-") && keyStartRe.MatchString(line) {
				// 保存之前的键值对
				if currentKey != "" {
					if _, exists := resMap[currentKey]; !exists {
						if lastIdx >= len(keys)-1 {
							keys = append(keys, currentKey)
							lastIdx = len(keys) - 1
						} else {
							keys = append(keys[:lastIdx+1], append([]string{currentKey}, keys[lastIdx+1:]...)...)
							lastIdx += 1
						}
					}
					resMap[currentKey] = valueBuilder.String()
					valueBuilder.Reset()
				}

				parts := commaRe.Split(line, 2)
				currentKey = parts[0]
				if len(parts) > 1 {
					valueBuilder.WriteString(parts[1])
				}
			} else {
				valueBuilder.WriteString("\n")
				valueBuilder.WriteString(line)
			}
		}

		// 保存最后一个键值对
		if currentKey != "" {
			if _, exists := resMap[currentKey]; !exists {
				if lastIdx >= len(keys)-1 {
					keys = append(keys, currentKey)
					lastIdx = len(keys) - 1
				} else {
					keys = append(keys[:lastIdx+1], append([]string{currentKey}, keys[lastIdx+1:]...)...)
					lastIdx += 1
				}
			}
			resMap[currentKey] = valueBuilder.String()
		}
	}

	// 生成最终的yaml字符串
	var result strings.Builder
	for _, key := range keys {
		if _, ok := skips[key]; ok {
			continue
		}
		value := strings.TrimRight(resMap[key], "\n\r")
		result.WriteString(key + ": " + value + "\n")
	}

	return result.String(), nil
}
