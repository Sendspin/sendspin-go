// ABOUTME: Linear resampler for converting between audio sample rates
// ABOUTME: Uses linear interpolation to convert interleaved audio samples
package audio

// Resampler performs linear interpolation to convert between sample rates
type Resampler struct {
	inputRate  int
	outputRate int
	channels   int
	ratio      float64
	position   float64
	lastSample []int32 // one sample per channel
}

// NewResampler creates a new resampler
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

// Resample converts input samples to output sample rate using linear interpolation
// input: interleaved samples at inputRate
// output: interleaved samples at outputRate
// Returns the number of output samples written.
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

		// Linear interpolation factor
		frac := inputPos - float64(inputIdx)

		// Interpolate each channel
		for ch := 0; ch < r.channels; ch++ {
			sample1 := input[inputIdx*r.channels+ch]
			sample2 := input[(inputIdx+1)*r.channels+ch]

			// Linear interpolation
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

// Reset resets the resampler state
func (r *Resampler) Reset() {
	r.position = 0.0
	for i := range r.lastSample {
		r.lastSample[i] = 0
	}
}

// OutputSamplesNeeded calculates how many output samples will be produced from input samples
func (r *Resampler) OutputSamplesNeeded(inputSamples int) int {
	inputFrames := inputSamples / r.channels
	outputFrames := int(float64(inputFrames) / r.ratio)
	return outputFrames * r.channels
}

// InputSamplesNeeded calculates how many input samples are needed to produce output samples
func (r *Resampler) InputSamplesNeeded(outputSamples int) int {
	outputFrames := outputSamples / r.channels
	inputFrames := int(float64(outputFrames) * r.ratio)
	return inputFrames * r.channels
}
