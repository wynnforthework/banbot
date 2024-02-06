package utils

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"regexp"
	"strings"
)

func SnakeToCamel(input string) string {
	parts := strings.Split(input, "_")
	caser := cases.Title(language.English)
	for i, text := range parts {
		parts[i] = caser.String(text)
	}
	return strings.Join(parts, "")
}

var (
	reCoinSplit = regexp.MustCompile("[/:-]")
)

/*
SplitSymbol
返回：Base，Quote，Settle，Identifier
*/
func SplitSymbol(pair string) (string, string, string, string) {
	parts := reCoinSplit.Split(pair, -1)
	settle, ident := "", ""
	if len(parts) > 2 {
		settle = parts[2]
	}
	if len(parts) > 3 {
		ident = parts[3]
	}
	return parts[0], parts[1], settle, ident
}
