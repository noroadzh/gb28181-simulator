// G.711 μ-law codec (8 kHz mono), the audio profile GB/T 28181 uses for talk.
//
// Browsers have no PCMU encoder, and the arithmetic is small enough to keep in
// the front end rather than adding a transcode step on the server.
//
// The mapping is lossy by design: decode(encode(x)) is the nearest μ-law step,
// not x itself. The sample in the comment below pins the curve.

const BIAS = 0x84
const CLIP = 32635

// encodeSample turns one 16-bit linear PCM sample into its μ-law byte.
export function encodeSample (sample) {
  let s = Math.max(-32768, Math.min(32767, sample | 0))
  const sign = s < 0 ? 0x80 : 0
  if (s < 0) s = -s
  if (s > CLIP) s = CLIP
  s += BIAS
  let exponent = 7
  for (let mask = 0x4000; exponent > 0 && (s & mask) === 0; mask >>= 1) exponent--
  const mantissa = (s >> (exponent + 3)) & 0x0f
  return (sign | (exponent << 4) | mantissa) ^ 0xff
}

// decodeSample turns one μ-law byte back into a 16-bit linear sample.
export function decodeSample (ulaw) {
  const u = ~ulaw & 0xff
  const sign = u & 0x80 ? -1 : 1
  const exponent = (u >> 4) & 0x07
  const mantissa = u & 0x0f
  let sample = ((mantissa << 3) + BIAS) << exponent
  sample -= BIAS
  return sign < 0 ? -sample : sample
}

// encode maps 16-bit samples to μ-law bytes.
export function encode (samples) {
  const out = new Uint8Array(samples.length)
  for (let i = 0; i < samples.length; i++) out[i] = encodeSample(samples[i])
  return out
}

// decode maps μ-law bytes to 16-bit samples.
export function decode (bytes) {
  const out = new Int16Array(bytes.length)
  for (let i = 0; i < bytes.length; i++) out[i] = decodeSample(bytes[i])
  return out
}
