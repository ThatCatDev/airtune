package raop

import (
	"fmt"
	"math"
)

// LinearToAirPlay converts a linear volume (0.0 to 1.0) to the AirPlay dB
// scale (-144.0 to 0.0) used in RAOP SET_PARAMETER requests.
//
// The conversion uses a logarithmic curve: dB = 30 * log10(linear), which maps
// linear 1.0 to 0 dB (maximum volume). The result is clamped to a minimum of
// -144.0, which represents silence/mute on AirPlay devices.
func LinearToAirPlay(linear float64) float64 {
	if linear <= 0.0 {
		return -144.0
	}
	db := 30.0 * math.Log10(linear)
	if db < -144.0 {
		return -144.0
	}
	return db
}

// AirPlayToLinear converts an AirPlay dB volume (-144.0 to 0.0) back to a
// linear volume (0.0 to 1.0).
//
// At -144.0 dB or below, 0.0 is returned (silence). The inverse formula is
// linear = 10^(db/30).
func AirPlayToLinear(db float64) float64 {
	if db <= -144.0 {
		return 0.0
	}
	return math.Pow(10.0, db/30.0)
}

// FormatVolume formats a dB volume value for use in a RAOP SET_PARAMETER
// request body. The format is "volume: <value>\r\n" with six decimal places,
// as expected by AirPlay receivers.
func FormatVolume(db float64) string {
	return fmt.Sprintf("volume: %f\r\n", db)
}
