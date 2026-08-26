package voice

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

// The browser decodes this in one call, so the header has to be right — a
// wrong sample rate is a chipmunk, a wrong data length is a truncated sample.
func TestWavFromPCM(t *testing.T) {
	pcm := make([]byte, 960) // 480 samples
	wav := wavFromPCM(pcm, 24000)

	if got := string(wav[0:4]); got != "RIFF" {
		t.Errorf("chunk id = %q, want RIFF", got)
	}
	if got := string(wav[8:12]); got != "WAVE" {
		t.Errorf("format = %q, want WAVE", got)
	}
	if got := string(wav[12:16]); got != "fmt " {
		t.Errorf("subchunk = %q, want 'fmt '", got)
	}
	if got := binary.LittleEndian.Uint16(wav[20:22]); got != 1 {
		t.Errorf("audio format = %d, want 1 (PCM)", got)
	}
	if got := binary.LittleEndian.Uint16(wav[22:24]); got != 1 {
		t.Errorf("channels = %d, want 1 (mono)", got)
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 24000 {
		t.Errorf("sample rate = %d, want 24000", got)
	}
	if got := binary.LittleEndian.Uint16(wav[34:36]); got != 16 {
		t.Errorf("bits per sample = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(wav[40:44]); got != uint32(len(pcm)) {
		t.Errorf("data length = %d, want %d", got, len(pcm))
	}
	if len(wav) != 44+len(pcm) {
		t.Errorf("total = %d, want %d", len(wav), 44+len(pcm))
	}
	// The RIFF size field counts everything after it.
	if got := binary.LittleEndian.Uint32(wav[4:8]); got != uint32(36+len(pcm)) {
		t.Errorf("riff size = %d, want %d", got, 36+len(pcm))
	}
}

func TestWavRateFollowsTheEngine(t *testing.T) {
	// The echo engine answers at the input rate and a speech model at its own,
	// so the header must carry whatever it was given rather than a constant.
	wav := wavFromPCM(make([]byte, 4), InputSampleRate)
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != InputSampleRate {
		t.Errorf("sample rate = %d, want %d", got, InputSampleRate)
	}
}

// The loopback has no voice. Refusing is the honest answer; returning silence
// would look like a broken speaker.
func TestPreviewRefusesOnTheEchoBackend(t *testing.T) {
	_, err := Preview(context.Background(), Options{Backend: BackendEcho}, "Puck")
	if !errors.Is(err, ErrPreviewUnsupported) {
		t.Errorf("Preview() on echo = %v, want ErrPreviewUnsupported", err)
	}
}
