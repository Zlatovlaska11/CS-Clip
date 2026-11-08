package encoder

import (
	"fmt"
	"os/exec"
)

// CutClip cuts a segment from inputVideo and writes it to outputPath using
// stream copy (no re-encoding). startSec is the start offset; durationSec
// is how long the clip should be.
func CutClip(inputVideo string, startSec float64, durationSec float64, outputPath string) error {
	cmd := exec.Command("ffmpeg",
		"-y",
		"-ss", fmt.Sprintf("%.2f", startSec),
		"-i", inputVideo,
		"-t", fmt.Sprintf("%.2f", durationSec),
		"-c", "copy",
		outputPath,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg: %w\n%s", err, out)
	}
	return nil
}
