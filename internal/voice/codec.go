package voice

// EncodePCMU converts signed 16-bit little-endian mono PCM to one G.711
// mu-law byte per sample.
func EncodePCMU(pcm []byte) []byte {
	if len(pcm) < 2 {
		return nil
	}
	out := make([]byte, len(pcm)/2)
	EncodePCMUInto(out, pcm)
	return out
}

// EncodePCMUInto encodes pcm into dst, which must have room for len(pcm)/2
// samples. It returns the encoded prefix and lets the real-time call loop reuse
// one frame buffer instead of allocating at 50 frames per second.
func EncodePCMUInto(dst, pcm []byte) []byte {
	samples := len(pcm) / 2
	if samples > len(dst) {
		samples = len(dst)
	}
	dst = dst[:samples]
	for i := range dst {
		sample := int16(uint16(pcm[i*2]) | uint16(pcm[i*2+1])<<8)
		dst[i] = linearToMuLaw(sample)
	}
	return dst
}

// DecodePCMU expands G.711 mu-law bytes to signed 16-bit little-endian mono
// PCM.
func DecodePCMU(encoded []byte) []byte {
	out := make([]byte, len(encoded)*2)
	DecodePCMUInto(out, encoded)
	return out
}

// DecodePCMUInto expands encoded into dst, which must have room for two bytes
// per sample. It returns the decoded PCM prefix.
func DecodePCMUInto(dst, encoded []byte) []byte {
	samples := len(encoded)
	if samples*2 > len(dst) {
		samples = len(dst) / 2
	}
	dst = dst[:samples*2]
	for i, value := range encoded {
		if i >= samples {
			break
		}
		sample := muLawToLinear(value)
		dst[i*2] = byte(uint16(sample))
		dst[i*2+1] = byte(uint16(sample) >> 8)
	}
	return dst
}

// ScalePCM16LE changes the level of signed 16-bit little-endian mono PCM in
// place. Percent is clamped to 0-100 so normal voice processing cannot clip.
func ScalePCM16LE(pcm []byte, percent int) {
	percent = min(max(percent, 0), 100)
	if percent == 100 {
		return
	}
	for index := 0; index+1 < len(pcm); index += 2 {
		sample := int16(uint16(pcm[index]) | uint16(pcm[index+1])<<8)
		scaled := int16(int32(sample) * int32(percent) / 100)
		pcm[index] = byte(uint16(scaled))
		pcm[index+1] = byte(uint16(scaled) >> 8)
	}
}

func linearToMuLaw(sample int16) byte {
	const bias = 0x84
	const clip = 32635
	sign := byte(0)
	value := int(sample)
	if value < 0 {
		value = -value
		sign = 0x80
	}
	if value > clip {
		value = clip
	}
	value += bias
	exponent := 7
	for mask := 0x4000; exponent > 0 && value&mask == 0; mask >>= 1 {
		exponent--
	}
	mantissa := (value >> (exponent + 3)) & 0x0f
	return ^(sign | byte(exponent<<4) | byte(mantissa))
}

func muLawToLinear(encoded byte) int16 {
	value := ^encoded
	sign := value & 0x80
	exponent := int((value >> 4) & 0x07)
	mantissa := int(value & 0x0f)
	sample := ((mantissa << 3) + 0x84) << exponent
	sample -= 0x84
	if sign != 0 {
		sample = -sample
	}
	if sample > 32767 {
		sample = 32767
	}
	if sample < -32768 {
		sample = -32768
	}
	return int16(sample)
}
