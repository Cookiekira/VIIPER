package switch2pro

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeviceDescriptorMatchesCapture(t *testing.T) {
	desc := MakeDescriptor()
	require.Equal(t,
		mustHexTest(t, "12010002ef0201407e056920010101020301"),
		desc.Bytes(),
	)
}

func TestHIDReportDescriptorMatchesCapture(t *testing.T) {
	got, err := reportDescriptor.Bytes()
	require.NoError(t, err)
	require.Equal(t,
		mustHexTest(t, "05010905a101850505ff0901150026ff00953f750881028509090195028102050919012915250195157501810295017503810305010901a100093009310933093526ff0f9504750c8102c005ff090226ff0095347508810285020901953f9102c0"),
		[]byte(got),
	)
}

func TestMicrosoftOSStringDescriptor(t *testing.T) {
	desc := MakeDescriptor()
	require.Equal(t,
		mustHexTest(t, "12034d005300460054003100300030002000"),
		desc.RawStrings[msOSStringDescriptorIndex],
	)
}

func mustHexTest(t *testing.T, s string) []byte {
	t.Helper()
	out, err := hex.DecodeString(s)
	require.NoError(t, err)
	return out
}
