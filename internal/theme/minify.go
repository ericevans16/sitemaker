package theme

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	htmlBlockRe      = regexp.MustCompile(`(?is)<pre\b[^>]*>.*?</pre>|<textarea\b[^>]*>.*?</textarea>|<script\b[^>]*>.*?</script>|<style\b[^>]*>.*?</style>`)
	htmlTagWS        = regexp.MustCompile(`>\s+<`)
	blockSentinelFmt = "\x01BLOCK%04d\x01"
)

// MinifyHTML collapses inter-tag whitespace while leaving <pre>, <script>,
// <style> and <textarea> blocks untouched.
func MinifyHTML(data []byte) []byte {
	s := string(data)
	var blocks []string
	s = htmlBlockRe.ReplaceAllStringFunc(s, func(match string) string {
		blocks = append(blocks, match)
		return fmt.Sprintf(blockSentinelFmt, len(blocks)-1)
	})
	s = htmlTagWS.ReplaceAllString(s, "><")
	for i, block := range blocks {
		s = strings.Replace(s, fmt.Sprintf(blockSentinelFmt, i), block, 1)
	}
	return []byte(strings.TrimSpace(s))
}

// MinifyCSS strips comments and collapses whitespace.
func MinifyCSS(data []byte) []byte {
	s := stripCSSComments(string(data))
	var b strings.Builder
	b.Grow(len(s))
	inWS := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			if !inWS {
				b.WriteByte(' ')
				inWS = true
			}
			continue
		}
		inWS = false
		b.WriteRune(r)
	}
	return []byte(strings.TrimSpace(b.String()))
}

func stripCSSComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			i += 2
			for i+1 < len(s) && !(s[i] == '*' && s[i+1] == '/') {
				i++
			}
			i += 2
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
