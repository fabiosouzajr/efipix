package money

import "testing"
import "github.com/stretchr/testify/require"

func TestCentavosString(t *testing.T) {
	require.Equal(t, "10.50", Centavos(1050).String())
	require.Equal(t, "0.05", Centavos(5).String())
	require.Equal(t, "100.00", Centavos(10000).String())
}

func TestParseString(t *testing.T) {
	c, err := ParseString("10.50")
	require.NoError(t, err)
	require.Equal(t, Centavos(1050), c)

	c, err = ParseString("0.05")
	require.NoError(t, err)
	require.Equal(t, Centavos(5), c)

	_, err = ParseString("abc")
	require.Error(t, err)
}
