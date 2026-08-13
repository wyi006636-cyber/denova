package yanzhouprotocol

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

type Reader struct {
	input         *bufio.Reader
	maxFrameBytes int
}

func NewReader(input io.Reader, maxFrameBytes int) *Reader {
	if maxFrameBytes < 1 {
		maxFrameBytes = DefaultMaxFrameBytes
	}
	return &Reader{
		input:         bufio.NewReaderSize(input, maxFrameBytes+2),
		maxFrameBytes: maxFrameBytes,
	}
}

func (r *Reader) ReadFrame() (Envelope, error) {
	line, err := r.input.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) {
		return Envelope{}, protocolError(CodeFrameTooLarge, "frame exceeds configured byte limit", nil)
	}
	if errors.Is(err, io.EOF) {
		if len(line) == 0 {
			return Envelope{}, io.EOF
		}
		return Envelope{}, protocolError(CodeIncompleteFrame, "EOF with residual frame bytes", nil)
	}
	if err != nil {
		return Envelope{}, err
	}
	line = bytes.TrimSuffix(line, []byte{'\n'})
	if len(line) > r.maxFrameBytes {
		return Envelope{}, protocolError(CodeFrameTooLarge, "frame exceeds configured byte limit", nil)
	}
	if !utf8.Valid(line) {
		return Envelope{}, protocolError(CodeInvalidUTF8, "frame is not valid UTF-8", nil)
	}

	var frame Envelope
	if err := json.Unmarshal(line, &frame); err != nil {
		return Envelope{}, protocolError(CodeBadJSON, "frame is not valid JSON", err)
	}
	if err := frame.Validate(); err != nil {
		return Envelope{}, err
	}
	return frame, nil
}

func WriteFrame(output io.Writer, frame Envelope) error {
	if err := frame.Validate(); err != nil {
		return err
	}
	encoded, err := json.Marshal(frame)
	if err != nil {
		return protocolError(CodeInvalidFrame, "frame cannot be encoded", err)
	}
	if len(encoded) > DefaultMaxFrameBytes {
		return protocolError(CodeFrameTooLarge, "encoded frame exceeds configured byte limit", nil)
	}
	encoded = append(encoded, '\n')
	_, err = output.Write(encoded)
	return err
}
