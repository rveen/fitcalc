package derating

import (
	"math"
	"strings"
	"testing"

	"github.com/rveen/fitcalc/stress"
)

// TestLocalAmbient: a resistor heated by 20 K by the others has its power
// rating derated at 105 °C instead of 85 °C.
func TestLocalAmbient(t *testing.T) {
	ratings := map[string]string{"pmax": "0.1", "tmax": "155"}
	cold := comp("R", nil, stress.Stress{"p": q(0.05, "W")}, ratings)
	heated := comp("R", nil, stress.Stress{"p": q(0.05, "W"), "ta": q(20, "°C")}, ratings)

	if x := ratio(t, CheckAt(cold, 85), "P/Pmax"); math.Abs(x.Value-0.05/(0.1*70/85)) > 1e-9 {
		t.Errorf("at 85 °C: %g", x.Value)
	}
	x := ratio(t, CheckAt(heated, 85), "P/Pmax")
	if math.Abs(x.Value-0.05/(0.1*50/85)) > 1e-9 || !strings.Contains(x.Source, "at 105 °C") {
		t.Errorf("at 85 °C and 20 K: %g, %s", x.Value, x.Source)
	}
	if r := Check(heated); ratio(t, r, "P/Pmax").Value != 0.5 || len(r.Issues) != 0 {
		t.Errorf("the ratings as given depend on ta: %+v", r)
	}

	r := CheckAt(heated, 140)
	if len(r.Issues) != 1 || !strings.Contains(r.Issues[0].Text, "local ambient temperature (140 °C and 20 K from the other components) 160 °C at or above tmax 155 °C") {
		t.Errorf("at 140 °C and 20 K: %+v", r.Issues)
	}
}

// TestTemperatureRise: a transistor 40 K above its local ambient reaches
// tmax at 110 °C.
func TestTemperatureRise(t *testing.T) {
	c := comp("Q", nil, stress.Stress{"t": q(40, "°C"), "ta": q(5, "°C")}, map[string]string{"tmax": "150"})
	if r := CheckAt(c, 100); len(r.Issues) != 0 {
		t.Errorf("at 100 °C: %+v", r.Issues)
	}
	r := CheckAt(c, 110)
	if r.Level() != Overstress || len(r.Issues) != 1 || !strings.HasPrefix(r.Issues[0].Text, "temperature 155 °C (local ambient temperature (110 °C and 5 K from the other components) and a rise of 40 K) at or above tmax 150 °C") {
		t.Errorf("at 110 °C: %+v", r.Issues)
	}
}
