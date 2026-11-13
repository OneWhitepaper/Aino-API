package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// probeFundingAudioDuration inspects local uploaded bytes, never a user supplied
// URL. A subprocess deadline, disabled network protocols, and bounded diagnostic
// output keep malformed media from occupying an admission worker indefinitely.
func probeFundingAudioDuration(ctx context.Context, data []byte) (float64, error) {
	if len(data) == 0 {
		return 0, fundingRequestInvalid("audio upload is empty")
	}
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		if _, statErr := os.Stat("/opt/homebrew/bin/ffprobe"); statErr == nil {
			path = "/opt/homebrew/bin/ffprobe"
		} else {
			return 0, fundingPricingUnavailable("ffprobe is required to validate uploaded audio duration")
		}
	}
	file, err := os.CreateTemp("", "funding-audio-*")
	if err != nil {
		return 0, fmt.Errorf("create audio probe input: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return 0, fmt.Errorf("write audio probe input: %w", err)
	}
	if err = file.Close(); err != nil {
		return 0, err
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, path, "-v", "error", "-protocol_whitelist", "file,pipe", "-show_entries", "format=duration:stream=codec_type,duration", "-of", "json", "-i", file.Name())
	output, diagnostic := &fundingProbeOutput{}, &fundingProbeOutput{}
	command.Stdout, command.Stderr = output, diagnostic
	if err = command.Run(); err != nil {
		return 0, fundingRequestInvalid("uploaded audio could not be decoded or its duration probe timed out")
	}
	var result struct {
		Format  struct{ Duration string } `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Duration  string `json:"duration"`
		} `json:"streams"`
	}
	if json.Unmarshal(output.Bytes(), &result) != nil {
		return 0, fundingRequestInvalid("uploaded audio has invalid duration metadata")
	}
	seconds, _ := strconv.ParseFloat(result.Format.Duration, 64)
	audio := false
	for _, stream := range result.Streams {
		if stream.CodecType == "audio" {
			audio = true
			if duration, e := strconv.ParseFloat(stream.Duration, 64); e == nil {
				seconds = math.Max(seconds, duration)
			}
		}
	}
	if !audio || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, fundingRequestInvalid("uploaded audio must have a finite, positive, verifiable duration")
	}
	// ffprobe prints microsecond precision. Round the authorization upward.
	return math.Ceil(seconds*1e6) / 1e6, nil
}

type fundingProbeOutput struct{ bytes.Buffer }

func (b *fundingProbeOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 64<<10 {
		return 0, fmt.Errorf("audio probe output exceeds limit")
	}
	return b.Buffer.Write(data)
}
