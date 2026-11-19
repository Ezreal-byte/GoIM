package voice

import (
	"math"
	"testing"
)

func TestPCMURoundTripStaysWithinVoiceTolerance(t *testing.T) {
	const samples = 4096
	pcm := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		value := int16(math.Round(22000 * math.Sin(2*math.Pi*440*float64(i)/16000)))
		pcm[i*2] = byte(uint16(value))
		pcm[i*2+1] = byte(uint16(value) >> 8)
	}
	decoded := DecodePCMU(EncodePCMU(pcm))
	if len(decoded) != len(pcm) {
		t.Fatalf("decoded %d bytes, want %d", len(decoded), len(pcm))
	}
	var signal, noise float64
	for i := 0; i < samples; i++ {
		want := int16(uint16(pcm[i*2]) | uint16(pcm[i*2+1])<<8)
		got := int16(uint16(decoded[i*2]) | uint16(decoded[i*2+1])<<8)
		delta := float64(got - want)
		signal += float64(want) * float64(want)
		noise += delta * delta
	}
	snr := 10 * math.Log10(signal/noise)
	if snr < 30 {
		t.Fatalf("mu-law round trip SNR %.1f dB, want at least 30 dB", snr)
	}
}

func TestPCMUKnownSilenceAndLimits(t *testing.T) {
	if got := EncodePCMU([]byte{0, 0, 0xff, 0x7f, 0x00, 0x80}); len(got) != 3 {
		t.Fatalf("encoded %d bytes, want 3", len(got))
	}
	decoded := DecodePCMU([]byte{0xff, 0x7f, 0x80})
	if len(decoded) != 6 {
		t.Fatalf("decoded %d bytes, want 6", len(decoded))
	}
}

func TestScalePCM16LE(t *testing.T) {
	pcm := []byte{
		0xe8, 0x03, // 1000
		0x18, 0xfc, // -1000
		0xff, 0x7f, // 32767
		0x00, 0x80, // -32768
	}
	ScalePCM16LE(pcm, 50)
	want := []int16{500, -500, 16383, -16384}
	for index, expected := range want {
		got := int16(uint16(pcm[index*2]) | uint16(pcm[index*2+1])<<8)
		if got != expected {
			t.Fatalf("sample %d = %d, want %d", index, got, expected)
		}
	}
	ScalePCM16LE(pcm, 0)
	for index := range pcm {
		if pcm[index] != 0 {
			t.Fatalf("zero volume left byte %d as %d", index, pcm[index])
		}
	}
}
