package ns2proble

import "testing"

func TestUnpackStick12(t *testing.T) {
	data := packStick12(0x123, 0xABC)
	x, y, err := UnpackStick12(data)
	if err != nil {
		t.Fatal(err)
	}
	if x != 0x123 || y != 0xABC {
		t.Fatalf("got %#x/%#x, want 0x123/0xabc", x, y)
	}
}

func TestNormalizeStickWithCalibration(t *testing.T) {
	cal := &StickCalibration{
		CenterX: 2048,
		CenterY: 2048,
		MaxX:    1000,
		MaxY:    1000,
		MinX:    1000,
		MinY:    1000,
	}
	tests := []struct {
		name         string
		x, y         int
		wantX, wantY uint16
	}{
		{name: "center", x: 2048, y: 2048, wantX: StickCenter, wantY: StickCenter},
		{name: "max", x: 3048, y: 3048, wantX: StickMax, wantY: StickMax},
		{name: "min", x: 1048, y: 1048, wantX: StickMin, wantY: StickMin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x, y := NormalizeStick(tt.x, tt.y, cal)
			if x != tt.wantX || y != tt.wantY {
				t.Fatalf("got %d/%d, want %d/%d", x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestMapCommonButtons(t *testing.T) {
	buttons := MapCommonButtons([]byte{0x8F, 0x73, 0xCF, 0x13})
	for _, want := range []uint32{
		ButtonY, ButtonX, ButtonB, ButtonA,
		ButtonZR, ButtonMinus, ButtonPlus, ButtonHome, ButtonCapture, ButtonC,
		ButtonDown, ButtonUp, ButtonRight, ButtonLeft, ButtonL, ButtonZL,
		ButtonGR, ButtonGL, ButtonHeadset,
	} {
		if buttons&want == 0 {
			t.Fatalf("mapped buttons %#x missing %#x", buttons, want)
		}
	}
}

func TestParseCommonReport(t *testing.T) {
	report := make([]byte, 0x3C)
	copy(report[0x04:0x08], []byte{0x08, 0x10, 0x40, 0x02})
	copy(report[0x0A:0x0D], packStick12(2500, 1500))
	copy(report[0x0D:0x10], packStick12(100, 3900))
	report[0x1F] = 0xA0
	report[0x20] = 0x0F
	report[0x21] = 0x34
	report[0x30] = 0x34
	report[0x31] = 0x12
	report[0x36] = 0x78
	report[0x37] = 0x56

	st, err := ParseCommonReport(report, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Buttons&(ButtonA|ButtonHome|ButtonL|ButtonGL) != ButtonA|ButtonHome|ButtonL|ButtonGL {
		t.Fatalf("buttons not mapped: %#x", st.Buttons)
	}
	if st.LX != 2500 || st.LY != 1500 || st.RX != 100 || st.RY != 3900 {
		t.Fatalf("sticks = %d/%d %d/%d", st.LX, st.LY, st.RX, st.RY)
	}
	if !st.Charging || !st.ExternalPower || st.BatteryLevel == 0 {
		t.Fatalf("battery fields not populated: %+v", st)
	}
	if st.AccelX != 0x1234 || st.GyroX != 0x5678 {
		t.Fatalf("IMU fields not parsed: %+v", st)
	}
}

func packStick12(x, y int) []byte {
	return []byte{
		byte(x),
		byte((x>>8)&0x0F) | byte((y&0x0F)<<4),
		byte(y >> 4),
	}
}
