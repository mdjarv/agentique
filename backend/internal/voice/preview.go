package voice

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// PreviewLine is what a voice says when auditioned.
//
// A line from the job rather than "hello": you are choosing a voice to hear say
// this kind of thing, so the sample should be that kind of thing. It also gives
// the ear a question's rising intonation, which is where voices differ most.
const PreviewLine = "Right, I've got that. Shall I go ahead and run it?"

const (
	// previewTimeout bounds one audition. Connect plus first audio is well
	// under a second in practice; this is the point at which a button that has
	// not made a sound is broken rather than slow.
	previewTimeout = 20 * time.Second

	// maxPreviewBytes caps a sample. At 24kHz mono s16le this is about eight
	// seconds — far more than the line needs, and a ceiling on what a stuck
	// engine can stream into memory.
	maxPreviewBytes = 400 << 10
)

// ErrPreviewUnsupported means the configured backend has no voice to audition.
var ErrPreviewUnsupported = errors.New("this voice backend cannot synthesise a preview")

// Preview synthesises one line in the given voice and returns it as WAV.
//
// It runs through the *same* engine a call uses, with the same session config,
// so the audition is the thing itself rather than an approximation from some
// other endpoint that might not match.
//
// The cost is a short real session per click, which is why the caller rate
// limits and why the sample is one sentence.
func Preview(ctx context.Context, opts Options, voiceName string) ([]byte, error) {
	switch opts.Backend {
	case BackendAIStudio, BackendVertex:
	default:
		return nil, ErrPreviewUnsupported
	}

	ctx, cancel := context.WithTimeout(ctx, previewTimeout)
	defer cancel()

	persona := Persona{VoiceName: voiceName}.Sanitize()
	opts.Persona = persona

	// No drafter instruction: an audition should read the line, not decide it
	// is being asked to draft a prompt and start conversing.
	engine, err := newGeminiEngine(ctx, opts,
		"You are a text to speech voice. Read the user's text back exactly, once, and say nothing else.",
		slog.With("subsystem", "voice", "preview", voiceName))
	if err != nil {
		return nil, fmt.Errorf("preview connect: %w", err)
	}
	defer engine.Close()

	if err := engine.SendText("Read this aloud exactly: " + PreviewLine); err != nil {
		return nil, fmt.Errorf("preview send: %w", err)
	}

	var pcm []byte
collect:
	for {
		select {
		case <-ctx.Done():
			break collect
		case ev, ok := <-engine.Events():
			if !ok {
				break collect
			}
			switch e := ev.(type) {
			case AudioEvent:
				pcm = append(pcm, e.PCM...)
				if len(pcm) >= maxPreviewBytes {
					break collect
				}
			case TurnCompleteEvent:
				break collect
			case ErrorEvent:
				return nil, fmt.Errorf("preview engine: %w", e.Err)
			}
		}
	}

	if len(pcm) == 0 {
		return nil, errors.New("the voice produced no audio")
	}
	return wavFromPCM(pcm, engine.SampleRate()), nil
}

// wavFromPCM wraps Int16 little-endian mono samples in a RIFF header.
//
// WAV rather than raw PCM because the browser can then decode it with one call
// and play it from a blob, instead of reproducing the call's scheduling queue
// for a two-second sample.
func wavFromPCM(pcm []byte, sampleRate int) []byte {
	const (
		numChannels   = 1
		bitsPerSample = 16
		headerSize    = 44
	)
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8

	out := make([]byte, 0, headerSize+len(pcm))
	out = append(out, "RIFF"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(36+len(pcm)))
	out = append(out, "WAVE"...)

	out = append(out, "fmt "...)
	out = binary.LittleEndian.AppendUint32(out, 16) // PCM subchunk size
	out = binary.LittleEndian.AppendUint16(out, 1)  // format: PCM
	out = binary.LittleEndian.AppendUint16(out, numChannels)
	out = binary.LittleEndian.AppendUint32(out, uint32(sampleRate))
	out = binary.LittleEndian.AppendUint32(out, uint32(byteRate))
	out = binary.LittleEndian.AppendUint16(out, uint16(blockAlign))
	out = binary.LittleEndian.AppendUint16(out, bitsPerSample)

	out = append(out, "data"...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(pcm)))
	return append(out, pcm...)
}
