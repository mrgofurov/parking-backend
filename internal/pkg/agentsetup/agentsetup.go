// Package agentsetup embeds a device-agent's settings (the agent.env text)
// into the agent executable, so a parking PC needs only one downloaded file:
// no agent.env to create or edit by hand.
//
// The settings are appended after the executable as an overlay, which
// Windows and the Go runtime ignore when running the program:
//
//	<exe bytes> <env text> <env length: uint64 little-endian> <magic: 8 bytes>
package agentsetup

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

const (
	magic      = "PKAGENT1"
	trailerLen = 8 + len(magic)
	// MaxEnvSize keeps a corrupt trailer from making Read allocate a lot.
	MaxEnvSize = 64 << 10
)

// Append returns exe with env embedded. Settings already embedded in exe are
// replaced, so a configured executable can be used as the template too.
func Append(exe []byte, env string) ([]byte, error) {
	if len(env) > MaxEnvSize {
		return nil, errors.New("agentsetup: sozlama juda katta")
	}
	base, _, _ := split(exe)
	out := make([]byte, 0, len(base)+len(env)+trailerLen)
	out = append(out, base...)
	out = append(out, env...)
	out = binary.LittleEndian.AppendUint64(out, uint64(len(env)))
	out = append(out, magic...)
	return out, nil
}

// split separates embedded settings from the executable.
func split(data []byte) (exe []byte, env string, ok bool) {
	if len(data) < trailerLen || !bytes.Equal(data[len(data)-len(magic):], []byte(magic)) {
		return data, "", false
	}
	n := binary.LittleEndian.Uint64(data[len(data)-trailerLen:])
	if n > MaxEnvSize || int(n) > len(data)-trailerLen {
		return data, "", false
	}
	end := len(data) - trailerLen
	return data[:end-int(n)], string(data[end-int(n) : end]), true
}

// Read returns the settings embedded in the file at path ("" when there are
// none). Only the end of the file is read.
func Read(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := st.Size()
	if size < int64(trailerLen) {
		return "", nil
	}
	tail := make([]byte, trailerLen)
	if _, err := f.ReadAt(tail, size-int64(trailerLen)); err != nil {
		return "", err
	}
	if !bytes.Equal(tail[8:], []byte(magic)) {
		return "", nil
	}
	n := binary.LittleEndian.Uint64(tail[:8])
	if n > MaxEnvSize || int64(n) > size-int64(trailerLen) {
		return "", errors.New("agentsetup: buzilgan sozlama bloki")
	}
	env := make([]byte, n)
	if _, err := f.ReadAt(env, size-int64(trailerLen)-int64(n)); err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return string(env), nil
}
