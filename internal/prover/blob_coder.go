package prover

import "fmt"

const (
	shastaBlobEncodingVersion = 0
	shastaBytesPerBlob        = 131072
	shastaBlobMaxDataSize     = (4*31+3)*1024 - 4
	shastaBlobEncodingRounds  = 1024
)

func decodeShastaBlob(raw []byte) ([]byte, error) {
	if len(raw) != shastaBytesPerBlob {
		return nil, fmt.Errorf("invalid blob length: got %d want %d", len(raw), shastaBytesPerBlob)
	}
	if raw[1] != shastaBlobEncodingVersion {
		return nil, fmt.Errorf("invalid blob encoding")
	}

	length := int(uint32(raw[2])<<16 | uint32(raw[3])<<8 | uint32(raw[4]))
	if length > shastaBlobMaxDataSize {
		return nil, fmt.Errorf("invalid blob encoding")
	}

	output := make([]byte, shastaBlobMaxDataSize)
	copy(output[0:27], raw[5:32])

	outputPos := 28
	inputPos := 32
	encodedByte := [4]byte{raw[0]}

	for i := 1; i < len(encodedByte); i++ {
		enc, newOutputPos, newInputPos, ok := decodeShastaBlobFieldElement(raw, output, outputPos, inputPos)
		if !ok {
			return nil, fmt.Errorf("invalid blob encoding")
		}
		encodedByte[i] = enc
		outputPos = newOutputPos
		inputPos = newInputPos
	}
	outputPos = reassembleShastaBlobEncodedBytes(outputPos, encodedByte, output)

	for round := 1; round < shastaBlobEncodingRounds; round++ {
		if outputPos >= length {
			break
		}
		for i := range encodedByte {
			enc, newOutputPos, newInputPos, ok := decodeShastaBlobFieldElement(raw, output, outputPos, inputPos)
			if !ok {
				return nil, fmt.Errorf("invalid blob encoding")
			}
			encodedByte[i] = enc
			outputPos = newOutputPos
			inputPos = newInputPos
		}
		outputPos = reassembleShastaBlobEncodedBytes(outputPos, encodedByte, output)
	}

	for _, b := range output[length:] {
		if b != 0 {
			return nil, fmt.Errorf("invalid blob encoding")
		}
	}
	for _, b := range raw[inputPos:] {
		if b != 0 {
			return nil, fmt.Errorf("invalid blob encoding")
		}
	}

	return output[:length], nil
}

func decodeShastaBlobFieldElement(
	data []byte,
	output []byte,
	outputPos int,
	inputPos int,
) (byte, int, int, bool) {
	if inputPos+32 > len(data) || outputPos+31 > len(output) {
		return 0, 0, 0, false
	}

	header := data[inputPos]
	if header&0xc0 != 0 {
		return 0, 0, 0, false
	}

	copy(output[outputPos:outputPos+31], data[inputPos+1:inputPos+32])
	return header, outputPos + 32, inputPos + 32, true
}

func reassembleShastaBlobEncodedBytes(outputPos int, encodedByte [4]byte, output []byte) int {
	outputPos--
	x := (encodedByte[0] & 0x3f) | ((encodedByte[1] & 0x30) << 2)
	y := (encodedByte[1] & 0x0f) | ((encodedByte[3] & 0x0f) << 4)
	z := (encodedByte[2] & 0x3f) | ((encodedByte[3] & 0x30) << 2)
	output[outputPos-32] = z
	output[outputPos-(32*2)] = y
	output[outputPos-(32*3)] = x
	return outputPos
}
