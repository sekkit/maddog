package builtin

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPostWriteReceiptUTF8Bounds(t *testing.T) {
	span := clipPostWriteSpan(strings.Repeat("界", maxCapturedReceiptSpanBytes), maxCapturedReceiptSpanBytes)
	if len(span) > maxCapturedReceiptSpanBytes || !utf8.ValidString(span) {
		t.Fatalf("span bytes=%d validUTF8=%v, want <=%d and valid", len(span), utf8.ValidString(span), maxCapturedReceiptSpanBytes)
	}
	receipt := renderPostWriteReceipts([]editReplacementReceipt{{matched: strings.Repeat("旧", 2000), replacement: strings.Repeat("新", 2000), occurrences: 1}})
	if len(receipt) > maxPostWriteReceiptBytes || !utf8.ValidString(receipt) {
		t.Fatalf("receipt bytes=%d validUTF8=%v, want <=%d and valid", len(receipt), utf8.ValidString(receipt), maxPostWriteReceiptBytes)
	}
}
