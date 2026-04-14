// ABOUTME: Simple linear resampler for converting audio sample rates
// ABOUTME: Used to convert MP3 files to 48kHz for Opus encoding
package server

// Resampler performs linear interpolation to convert between sample rates
type Resampler struct {
	inputRate  int
	outputRate int
	channels   int
	ratio      float64
	position   float64
	lastSample []int32 // one sample per channel
}

func NewResampler(inputRate, outputRate, channels int) *Resampler {
	return &Resampler{
		inputRate:  inputRate,
		outputRate: outputRate,
		channels:   channels,
		ratio:      float64(inputRate) / float64(outputRate),
		position:   0.0,
		lastSample: make([]int32, channels),
	}
}

// Resample converts interleaved input samples at inputRate to outputRate via linear interpolation.
func (r *Resampler) Resample(input []int32, output []int32) int {
	if len(input) == 0 {
		return 0
	}

	inputFrames := len(input) / r.channels
	outputFrames := len(output) / r.channels

	outIdx := 0

	for outIdx < outputFrames {
		// Calculate which input frame we need
		inputPos := r.position
		inputIdx := int(inputPos)

		// If we've consumed all input, stop
		if inputIdx >= inputFrames-1 {
			break
		}

		frac := inputPos - float64(inputIdx)

		for ch := 0; ch < r.channels; ch++ {
			sample1 := input[inputIdx*r.channels+ch]
			sample2 := input[(inputIdx+1)*r.channels+ch]

			interpolated := float64(sample1)*(1.0-frac) + float64(sample2)*frac
			output[outIdx*r.channels+ch] = int32(interpolated)
		}

		outIdx++
		r.position += r.ratio
	}

	// Reset position for next chunk, keeping fractional part
	r.position -= float64(int(r.position))

	return outIdx * r.channels
}

func (r *Resampler) Reset() {
	r.position = 0.0
	for i := range r.lastSample {
		r.lastSample[i] = 0
	}
}

func (r *Resampler) OutputSamplesNeeded(inputSamples int) int {
	inputFrames := inputSamples / r.channels
	outputFrames := int(float64(inputFrames) / r.ratio)
	return outputFrames * r.channels
}

func (r *Resampler) InputSamplesNeeded(outputSamples int) int {
	outputFrames := outputSamples / r.channels
	inputFrames := int(float64(outputFrames) * r.ratio)
	return inputFrames * r.channels
}
