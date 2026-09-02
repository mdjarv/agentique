package service

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// The task XML must reach schtasks as UTF-16LE with a BOM, and the declaration
// must agree. A UTF-8 file declaring UTF-8 fails at task creation with
// "(1,40) unable to switch the encoding" — schtasks widens the bytes before
// MSXML parses them — so this pins both halves of the contract.
func TestTaskXMLIsUTF16(t *testing.T) {
	var buf strings.Builder
	if err := taskTemplate.Execute(&buf, struct {
		BinaryPath, Arguments, UserID, Description string
	}{`C:\Users\m\.local\bin\agentique.exe`, "serve", `HOST\m`, "Agentique"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), `<?xml version="1.0" encoding="UTF-16"?>`) {
		t.Errorf("declaration must say UTF-16 to match the file encoding; got %q", buf.String()[:60])
	}

	enc := utf16leBOM(buf.String())
	if len(enc) < 2 || enc[0] != 0xFF || enc[1] != 0xFE {
		t.Fatalf("missing UTF-16LE BOM, got % X", enc[:2])
	}
	if len(enc)%2 != 0 {
		t.Fatalf("odd byte count %d — not UTF-16", len(enc))
	}
	units := make([]uint16, 0, (len(enc)-2)/2)
	for i := 2; i < len(enc); i += 2 {
		units = append(units, uint16(enc[i])|uint16(enc[i+1])<<8)
	}
	if got := string(utf16.Decode(units)); got != buf.String() {
		t.Error("UTF-16 round-trip does not reproduce the rendered XML")
	}
}
