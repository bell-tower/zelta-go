package backup

import (
	"fmt"
	"strings"
)

func compatibleSendFlags(st *Step, flags string) (string, string) {
	sourceEncrypted := encryptionEnabled(st.SrcEncryption)
	targetEncrypted := encryptionEnabled(st.TgtEncryption)
	incompatible := sourceEncrypted != targetEncrypted
	if sourceEncrypted && targetEncrypted && st.SourceStart != "" && st.MatchIVSet == "" {
		incompatible = true
	}
	if !incompatible {
		return flags, ""
	}
	flags = removeSendFeature(flags, "-e")
	if sourceEncrypted {
		at := st.Match
		if at == "" {
			at = st.SourceEnd
		}
		return flags, fmt.Sprintf("raw send unavailable at %s; falling back to decrypted send: %s", at, st.SrcName)
	}
	return flags, ""
}

func encryptionEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "-", "0", "off", "none":
		return false
	default:
		return true
	}
}

func removeSendFeature(flags, feature string) string {
	var out []string
	for _, token := range strings.Fields(flags) {
		if token == feature || token == "--embed" || token == "--embedded-data" {
			continue
		}
		if strings.HasPrefix(token, "-") && !strings.HasPrefix(token, "--") {
			var short strings.Builder
			short.WriteByte('-')
			for _, c := range token[1:] {
				if c != 'e' {
					short.WriteRune(c)
				}
			}
			token = short.String()
			if token == "-" {
				continue
			}
		}
		out = append(out, token)
	}
	return strings.Join(out, " ")
}
